package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"chainguard.dev/apko/pkg/build"
	"chainguard.dev/apko/pkg/build/types"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	v1tar "github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/imjasonh/kontain.me/pkg/serve"
	"gopkg.in/yaml.v2"
)

func main() {
	ctx := context.Background()
	st, err := serve.NewStorage(ctx)
	if err != nil {
		log.Fatalf("serve.NewStorage: %v", err)
	}
	http.Handle("/v2/", &server{
		info:    log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error:   log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
		storage: st,
	})
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/apko", http.StatusSeeOther))

	log.Println("Starting...")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}
	log.Printf("Listening on port %s", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

type server struct {
	info, error *log.Logger
	storage     *serve.Storage
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.info.Println("handler:", r.Method, r.URL)
	path := strings.TrimPrefix(r.URL.String(), "/v2/")

	switch {
	case path == "":
		// API Version check.
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		return
	case strings.Contains(path, "/blobs/"),
		strings.Contains(path, "/manifests/sha256:"):
		// Extract requested blob digest and redirect to serve it from GCS.
		// If it doesn't exist, this will return 404.
		parts := strings.Split(r.URL.Path, "/")
		digest := parts[len(parts)-1]
		serve.Blob(w, r, digest)
	case strings.Contains(path, "/manifests/"):
		s.serveApkoManifest(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// apko.kontain.me/wolfi-baselayout/nginx -> apko build and serve
func (s *server) serveApkoManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	parts := strings.Split(path, "/")

	// "go get" the package
	tagOrDigest := parts[len(parts)-1]

	// If request is for image by digest, try to serve it from GCS.
	if strings.HasPrefix(tagOrDigest, "sha256:") {
		desc, err := s.storage.BlobExists(ctx, tagOrDigest)
		if err != nil {
			s.error.Printf("ERROR (storage.BlobExists): %s", err)
			serve.Error(w, serve.ErrNotFound)
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Docker-Content-Digest", tagOrDigest)
			w.Header().Set("Content-Type", string(desc.MediaType))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", desc.Size))
			return
		}
		serve.Blob(w, r, tagOrDigest)
		return
	}

	packages := parts[:len(parts)-2]
	var ic types.ImageConfiguration
	if packages[0] == "url" {
		resp, err := http.Get("https://" + strings.Join(packages[1:], "/"))
		if err != nil {
			s.error.Printf("ERROR (fetch): %s", err)
			serve.Error(w, err)
			return
		}
		defer resp.Body.Close()

		if err := yaml.NewDecoder(resp.Body).Decode(&ic); err != nil {
			s.error.Printf("ERROR (parse fetched IC): %s", err)
			serve.Error(w, err)
			return
		}
	} else {
		sort.Strings(packages)

		// TODO: no way to actually specify an ImageConfiguration... :-/
		if err := yaml.NewDecoder(strings.NewReader(fmt.Sprintf(`
contents:
  repositories:
  - https://packages.wolfi.dev/os
  keyring:
  - https://packages.wolfi.dev/os/wolfi-signing.rsa.pub
  packages: [%s]
`, strings.Join(packages, ",")))).Decode(&ic); err != nil {
			s.error.Printf("ERROR (parse generated IC): %s", err)
			serve.Error(w, err)
			return
		}
	}
	ck := cacheKey(packages)

	// Check if we've already got a manifest for this set of packages.
	if _, err := s.storage.BlobExists(ctx, ck); err == nil {
		s.info.Println("serving cached manifest:", ck)
		serve.Blob(w, r, ck)
		return
	}

	// Build the image.
	img, err := s.build(ctx, ic)
	if err != nil {
		s.error.Printf("ERROR (build): %s", err)
		serve.Error(w, err)
		return
	}

	if err := s.storage.ServeManifest(w, r, img, ck); err != nil {
		s.error.Printf("ERROR (storage.ServeIndex): %v", err)
		serve.Error(w, err)
	}
}

var amd64 = types.ParseArchitecture("amd64")

func (s *server) build(ctx context.Context, ic types.ImageConfiguration) (v1.Image, error) {
	wd, err := os.MkdirTemp("", "apko-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create working directory: %w", err)
	}
	defer os.RemoveAll(wd)

	bc, err := build.New(wd,
		build.WithImageConfiguration(ic),
		build.WithProot(true),
		build.WithArch(amd64), // TODO: multiarch
		build.WithBuildDate(time.Time{}.Format(time.RFC3339)),
		build.WithAssertions(build.RequireGroupFile(true), build.RequirePasswdFile(true)))
	if err != nil {
		return nil, err
	}

	if err := bc.Refresh(); err != nil {
		return nil, fmt.Errorf("failed to update build context for %q: %w", amd64, err)
	}

	layerTarGZ, err := bc.BuildLayer()
	if err != nil {
		return nil, fmt.Errorf("failed to build layer image for %q: %w", amd64, err)
	}
	// TODO(kaniini): clean up everything correctly for multitag scenario
	// defer os.Remove(layerTarGZ)

	v1Layer, err := v1tar.LayerFromFile(layerTarGZ)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI layer from tar.gz: %w", err)
	}

	adds := make([]mutate.Addendum, 0, 1)
	adds = append(adds, mutate.Addendum{
		Layer: v1Layer,
		History: v1.History{
			Author:    "apko",
			Comment:   "This is an apko single-layer image",
			CreatedBy: "apko",
			Created:   v1.Time{Time: time.Time{}},
		},
	})

	v1Image, err := mutate.Append(empty.Image, adds...)
	if err != nil {
		return empty.Image, fmt.Errorf("unable to append OCI layer to empty image: %w", err)
	}

	cfg, err := v1Image.ConfigFile()
	if err != nil {
		return empty.Image, fmt.Errorf("unable to get OCI config file: %w", err)
	}

	cfg = cfg.DeepCopy()
	cfg.Author = "apko.kontain.me"
	cfg.Architecture = "amd64" // TODO: multiarch
	cfg.OS = "linux"
	cfg.Config.Entrypoint = []string{"/bin/sh", "-l"}

	img, err := mutate.ConfigFile(v1Image, cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to update OCI config file: %w", err)
	}

	return img, nil
}

func cacheKey(packages []string) string {
	ck := []byte(strings.Join(packages, ","))
	return fmt.Sprintf("apko-%x", md5.Sum(ck))
}
