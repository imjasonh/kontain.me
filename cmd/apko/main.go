package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"chainguard.dev/apko/pkg/build"
	"chainguard.dev/apko/pkg/build/types"
	"github.com/chainguard-dev/go-apk/pkg/fs"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/imjasonh/gcpslog"
	"github.com/imjasonh/kontain.me/pkg/serve"
	"gopkg.in/yaml.v2"
)

func main() {
	ctx := context.Background()
	st, err := serve.NewStorage(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "serve.NewStorage", "err", err)
		os.Exit(1)
	}
	http.Handle("/v2/", gcpslog.WithCloudTraceContext(&server{storage: st}))
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/apko", http.StatusSeeOther))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		slog.InfoContext(ctx, "Defaulting port", "port", port)
	}
	slog.InfoContext(ctx, "Listening...", "port", port)
	slog.ErrorContext(ctx, "ListenAndServe", "err", http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

type server struct{ storage *serve.Storage }

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
			slog.ErrorContext(ctx, "storage.BlobExists", "err", err)
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
			slog.ErrorContext(ctx, "http.Get", "err", err)
			serve.Error(w, err)
			return
		}
		defer resp.Body.Close()

		if err := yaml.NewDecoder(resp.Body).Decode(&ic); err != nil {
			slog.ErrorContext(ctx, "yaml.Decode", "err", err)
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
			slog.ErrorContext(ctx, "yaml.Decode", "err", err)
			serve.Error(w, err)
			return
		}
	}
	ck := cacheKey(packages)

	// Check if we've already got a manifest for this set of packages.
	if _, err := s.storage.BlobExists(ctx, ck); err == nil {
		slog.InfoContext(ctx, "serving cached manifest", "ck", ck)
		serve.Blob(w, r, ck)
		return
	}

	// Build the image.
	img, err := s.build(ctx, ic)
	if err != nil {
		slog.ErrorContext(ctx, "build", "err", err)
		serve.Error(w, err)
		return
	}

	if err := s.storage.ServeManifest(w, r, img, ck); err != nil {
		slog.ErrorContext(ctx, "storage.ServeManifest", "err", err)
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

	bc, err := build.New(ctx, fs.DirFS(wd),
		build.WithImageConfiguration(ic),
		build.WithArch(amd64), // TODO: multiarch
		build.WithBuildDate(time.Time{}.Format(time.RFC3339)),
		build.WithAssertions(build.RequireGroupFile(true), build.RequirePasswdFile(true)))
	if err != nil {
		return nil, err
	}

	_, layer, err := bc.BuildLayer(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build layer image for %q: %w", amd64, err)
	}

	adds := make([]mutate.Addendum, 0, 1)
	adds = append(adds, mutate.Addendum{
		Layer: layer,
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
