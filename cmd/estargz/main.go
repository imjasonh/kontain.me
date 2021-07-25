package main

import (
	"context"
	"errors"
	"fmt"
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
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/estargz", http.StatusSeeOther))

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
		if rng := r.Header.Get("Range"); rng != "" {
			s.info.Printf("Got blob request with Range: %s", rng)
		}
	case strings.Contains(path, "/manifests/"):
		s.serveEstartgzManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

func cacheKey(orig string) string { return fmt.Sprintf("estargz-%s", orig) }

// estargz.kontain.me/ubuntu -> estargz-optimize ubuntu and serve
func (s *server) serveEstartgzManifest(w http.ResponseWriter, r *http.Request) {
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
	for strings.HasPrefix(refstr, "estargz.kontain.me/") {
		refstr = strings.TrimPrefix(refstr, "estargz.kontain.me/")
	}

	ref, err := name.ParseReference(refstr)
	if err != nil {
		s.error.Printf("ERROR (ParseReference(%q)): %v", refstr, err)
		serve.Error(w, err)
		return
	}

	var ck string

	// Determine whether the ref is for an image or index.
	desc, err := remote.Get(ref, remote.WithContext(ctx))
	if err != nil {
		s.error.Printf("ERROR (remote.Get): %v", err)
		serve.Error(w, err)
		return
	}

	// Check if we have a estargzed manifest cached (since HEAD failed
	// before), and if so serve it directly.
	ck = cacheKey(desc.Digest.String())
	if desc, err := s.storage.BlobExists(ctx, ck); err == nil {
		if r.Method == http.MethodHead {
			w.Header().Set("Docker-Content-Digest", desc.Digest.String())
			w.Header().Set("Content-Type", string(desc.MediaType))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", desc.Size))
			return
		}

		s.info.Println("serving cached manifest:", ck)
		serve.Blob(w, r, ck)
		return
	}

	if err := s.estargz(w, r, desc, ck); err != nil {
		s.error.Printf("ERROR (estargz): %v", err)
		serve.Error(w, err)
		return
	}
}

func (s *server) estargz(w http.ResponseWriter, r *http.Request, desc *remote.Descriptor, ck string) error {
	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList:
		idx, err := desc.ImageIndex()
		if err != nil {
			return err
		}
		idx, err = optimizeIndex(idx)
		if err != nil {
			return fmt.Errorf("failed to optimize index: %v", err)
		}
		if err := s.storage.ServeIndex(w, r, idx, ck); err != nil {
			return err
		}

	case types.DockerManifestSchema1, types.DockerManifestSchema1Signed:
		return errors.New("docker schema 1 images are not supported")

	default:
		// Assume anything else is an image, since some registries don't set mediaTypes properly.
		img, err := desc.Image()
		if err != nil {
			return err
		}
		img, err = optimizeImage(img)
		if err != nil {
			return fmt.Errorf("failed to optimize image: %v", err)
		}
		if err := s.storage.ServeManifest(w, r, img, ck); err != nil {
			return err
		}
	}

	return nil
}

func optimizeImage(img v1.Image) (v1.Image, error) {
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	ocfg := cfg.DeepCopy()
	ocfg.History = nil
	ocfg.RootFS.DiffIDs = nil

	oimg, err := mutate.ConfigFile(empty.Image, ocfg)
	if err != nil {
		return nil, err
	}

	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}

	olayers := make([]mutate.Addendum, 0, len(layers))
	for _, layer := range layers {
		olayer, err := tarball.LayerFromOpener(layer.Uncompressed, tarball.WithEstargz)
		if err != nil {
			return nil, err
		}

		olayers = append(olayers, mutate.Addendum{
			Layer:     olayer,
			MediaType: types.DockerLayer,
		})
	}

	oimg, err = mutate.Append(oimg, olayers...)
	if err != nil {
		return nil, err
	}
	return oimg, nil
}

func optimizeIndex(idx v1.ImageIndex) (v1.ImageIndex, error) {
	im, err := idx.IndexManifest()
	if err != nil {
		return nil, err
	}

	// Build an image for each child from the base and append it to a new index to produce the result.
	adds := make([]mutate.IndexAddendum, 0, len(im.Manifests))
	for _, desc := range im.Manifests {
		img, err := idx.Image(desc.Digest)
		if err != nil {
			return nil, err
		}

		oimg, err := optimizeImage(img)
		if err != nil {
			return nil, err
		}
		adds = append(adds, mutate.IndexAddendum{
			Add: oimg,
			Descriptor: v1.Descriptor{
				URLs:        desc.URLs,
				MediaType:   desc.MediaType,
				Annotations: desc.Annotations,
				Platform:    desc.Platform,
			},
		})
	}

	idxType, err := idx.MediaType()
	if err != nil {
		return nil, err
	}

	return mutate.IndexMediaType(mutate.AppendManifests(empty.Index, adds...), idxType), nil
}
