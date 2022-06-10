package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
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
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/mirror", http.StatusSeeOther))

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

	w.Header().Set("Access-Control-Allow-Origin", "*")                 // Allow CORS requests from any domain.
	w.Header().Set("Access-Control-Expose-Headers", "*")               // Respond with all headers to requests from any domain.
	w.Header().Set("Access-Control-Allow-Credentials", "true")         // Allow... credentials? I guess?
	w.Header().Set("Access-Control-Allow-Methods", "GET,HEAD,OPTIONS") // Allow CORS requests to use these methods.
	w.Header().Set("Access-Control-Allow-Headers", "*")                // Allow all CORS headers in the OPTIONS request.
	if r.Method == http.MethodOptions {
		return
	}

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
		s.serveMirrorManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

// mirror.kontain.me/ubuntu -> mirror ubuntu and serve
func (s *server) serveMirrorManifest(w http.ResponseWriter, r *http.Request) {
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
	for strings.HasPrefix(refstr, "mirror.kontain.me/") {
		refstr = strings.TrimPrefix(refstr, "mirror.kontain.me/")
	}

	ref, err := name.ParseReference(refstr)
	if err != nil {
		s.error.Printf("ERROR (ParseReference(%q)): %v", refstr, err)
		serve.Error(w, err)
		return
	}

	// If it's a HEAD request, and request was by digest, and we have that
	// manifest mirrored by digest already, serve HEAD response from GCS.
	// If it's a HEAD request and the other conditions aren't met, we'll
	// handle this later by consulting the real registry.
	if r.Method == http.MethodHead {
		if d, ok := ref.(name.Digest); ok {
			if desc, err := s.storage.BlobExists(ctx, d.DigestStr()); err == nil {
				w.Header().Set("Docker-Content-Digest", d.DigestStr())
				w.Header().Set("Content-Type", string(desc.MediaType))
				w.Header().Set("Content-Length", fmt.Sprintf("%d", desc.Size))
				return
			}
		}
	}

	var idx v1.ImageIndex
	var img v1.Image

	// Get the original image's digest, and check if we have that manifest
	// blob.
	d, err := remote.Head(ref, remote.WithContext(ctx))
	if err != nil {
		s.error.Printf("ERROR (remote.Head(%q)): %v", ref, err)
		var desci interface {
			Digest() (v1.Hash, error)
			Size() (int64, error)
			MediaType() (types.MediaType, error)
		}
		// HEAD failed, let's figure out if it was an index or image by doing GETs.
		idx, err = remote.Index(ref, remote.WithContext(ctx))
		if err != nil {
			s.error.Printf("ERROR (remote.Index): %v", err)
			img, err = remote.Image(ref, remote.WithContext(ctx))
			if err != nil {
				s.error.Printf("ERROR (remote.Image): %v", err)
				serve.Error(w, err)
				return
			}
			desci = img
		} else {
			desci = idx
		}

		h, err := desci.Digest()
		if err != nil {
			s.error.Printf("ERROR (desc.Digest): %v", err)
			serve.Error(w, err)
			return
		}
		sz, err := desci.Size()
		if err != nil {
			s.error.Printf("ERROR (desc.Size): %v", err)
			serve.Error(w, err)
			return
		}
		mt, err := desci.MediaType()
		if err != nil {
			s.error.Printf("ERROR (desc.MediaType): %v", err)
			serve.Error(w, err)
			return
		}
		d = &v1.Descriptor{
			Digest:    h,
			MediaType: mt,
			Size:      sz,
		}
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Docker-Content-Digest", d.Digest.String())
		w.Header().Set("Content-Type", string(d.MediaType))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", d.Size))
		return
	}
	if _, err := s.storage.BlobExists(ctx, d.Digest.String()); err == nil {
		serve.Blob(w, r, d.Digest.String())
		return
	} else {
		s.info.Printf("INFO (serve.BlobExists(%q)): %v", d.Digest, err)
	}

	// Blob doesn't exist yet. Try to get the image manifest+layers
	// and cache them.
	switch d.MediaType {
	case types.OCIImageIndex, types.DockerManifestList:
		if idx == nil {
			// If the image is a manifest list, fetch and mirror
			// the image index.
			idx, err = remote.Index(ref, remote.WithContext(ctx))
			if err != nil {
				s.error.Printf("ERROR (remote.Index): %v", err)
				serve.Error(w, err)
				return
			}
		}
		if err := s.storage.ServeIndex(w, r, idx); err != nil {
			s.error.Printf("ERROR (storage.ServeIndex): %v", err)
			serve.Error(w, err)
			return
		}
	case types.OCIManifestSchema1, types.DockerManifestSchema2:
		if img == nil {
			// If it's a simple image, fetch and mirror its
			// manifest.
			img, err = remote.Image(ref, remote.WithContext(ctx))
			if err != nil {
				s.error.Printf("ERROR (remote.Image): %v", err)
				serve.Error(w, err)
				return
			}
		}
		if err := s.storage.ServeManifest(w, r, img); err != nil {
			s.error.Printf("ERROR (storage.ServeManifest): %v", err)
			serve.Error(w, err)
			return
		}
	default:
		err := fmt.Errorf("unknown media type: %s", d.MediaType)
		s.error.Printf("ERROR (serveMirrorManifest): %v", err)
		serve.Error(w, err)
	}
}
