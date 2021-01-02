package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/imjasonh/kontain.me/pkg/serve"
	"golang.org/x/sync/errgroup"
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
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/master/cmd/flatten", http.StatusSeeOther))

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
		s.serveFlattenManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

func cacheKey(orig string) string {
	return fmt.Sprintf("flatten-%s", orig)
}

// flatten.kontain.me/ubuntu -> flatten ubuntu and serve
func (s *server) serveFlattenManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	parts := strings.Split(path, "/")

	refstr := strings.Join(parts[:len(parts)-2], "/")
	tagOrDigest := parts[len(parts)-1]
	if strings.HasPrefix(tagOrDigest, "sha256:") {
		refstr += "@" + tagOrDigest
	} else {
		refstr += ":" + tagOrDigest
	}
	for strings.HasPrefix(refstr, "flatten.kontain.me/") {
		refstr = strings.TrimPrefix(refstr, "flatten.kontain.me/")
	}

	ref, err := name.ParseReference(refstr)
	if err != nil {
		s.error.Printf("ERROR (ParseReference(%q)): %v", refstr, err)
		serve.Error(w, err)
		return
	}

	// Determine whether the ref is for an image or index.
	d, err := remote.Head(ref)
	if err != nil {
		s.error.Printf("ERROR (remote.Head(%q)): %v", ref, err)
		serve.Error(w, err)
		return
	}

	// Check if we have a flattened manifest cached, and if so serve it
	// directly.
	ck := cacheKey(d.Digest.String())
	if err := s.storage.BlobExists(ctx, ck); err == nil {
		s.info.Println("serving cached manifest:", ck)
		serve.Blob(w, r, ck)
		return
	}

	switch d.MediaType {
	case types.DockerManifestList:
		idx, err := remote.Index(ref, remote.WithContext(ctx))
		if err != nil {
			s.error.Printf("ERROR (remote.Index): %v", err)
			serve.Error(w, err)
			return
		}
		im, err := idx.IndexManifest()
		if err != nil {
			s.error.Printf("ERROR (index.IndexManifest): %v", err)
			serve.Error(w, err)
			return
		}
		// Flatten each image in the manifest.
		var g errgroup.Group
		adds := make([]mutate.IndexAddendum, len(im.Manifests))
		for i, m := range im.Manifests {
			i, m := i, m
			g.Go(func() error {
				img, err := idx.Image(m.Digest)
				if err != nil {
					s.error.Printf("ERROR (idx.Image): %v", err)
					return err
				}
				fimg, err := s.flatten(img)
				if err != nil {
					return err
				}
				m.Digest, err = fimg.Digest()
				if err != nil {
					s.error.Printf("ERROR (fimg.Digest): %v", err)
					return err
				}
				adds[i] = mutate.IndexAddendum{
					Add:        fimg,
					Descriptor: m,
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			serve.Error(w, err)
			return
		}
		fidx := mutate.AppendManifests(empty.Index, adds...)
		if err := s.storage.ServeIndex(w, r, fidx, ck); err != nil {
			s.error.Printf("ERROR (storage.ServeIndex): %v", err)
			serve.Error(w, err)
			return
		}

	case types.DockerManifestSchema2:
		img, err := remote.Image(ref, remote.WithContext(ctx))
		if err != nil {
			s.error.Printf("ERROR (remote.Image): %v", err)
			serve.Error(w, err)
			return
		}

		fimg, err := s.flatten(img)
		if err != nil {
			serve.Error(w, err)
			return
		}

		if err := s.storage.ServeManifest(w, r, fimg, ck); err != nil {
			s.error.Printf("ERROR (storage.ServeManifest): %v", err)
			serve.Error(w, err)
			return
		}
	default:
		err := fmt.Errorf("unknown media type: %s", d.MediaType)
		s.error.Printf("ERROR (serveFlattenManifest): %v", err)
		serve.Error(w, err)
	}
}

func (s *server) flatten(img v1.Image) (v1.Image, error) {
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return mutate.Extract(img), nil })
	if err != nil {
		s.error.Printf("ERROR (tarball.LayerFromOpener): %v", err)
		return nil, err
	}
	fimg, err := mutate.AppendLayers(empty.Image, l)
	if err != nil {
		s.error.Printf("ERROR (mutate.AppendLayers): %v", err)
		return nil, err
	}

	// Copy over basic information from original config file.
	ocf, err := img.ConfigFile()
	if err != nil {
		s.error.Printf("ERROR (img.ConfigFile): %v", err)
		return nil, err
	}
	ncf, err := fimg.ConfigFile()
	if err != nil {
		s.error.Printf("ERROR (empty.Image.ConfigFile): %v", err)
		return nil, err
	}
	cf := ncf.DeepCopy()
	cf.Architecture = ocf.Architecture
	cf.OS = ocf.OS
	cf.OSVersion = ocf.OSVersion
	cf.Config = ocf.Config

	return mutate.ConfigFile(fimg, cf)
}
