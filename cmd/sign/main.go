package main

import (
	"context"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/imjasonh/kontain.me/pkg/serve"

	"github.com/sigstore/cosign/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/cmd/cosign/cli/sign"
	fulcioclient "github.com/sigstore/fulcio/pkg/client"
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
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/sign", http.StatusSeeOther))

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
		s.serveSignManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

// sign.kontain.me/ubuntu -> mirror ubuntu and serve
func (s *server) serveSignManifest(w http.ResponseWriter, r *http.Request) {
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
	for strings.HasPrefix(refstr, "sign.kontain.me/") {
		refstr = strings.TrimPrefix(refstr, "sign.kontain.me/")
	}

	ref, err := name.ParseReference(refstr)
	if err != nil {
		s.error.Printf("ERROR (ParseReference(%q)): %v", refstr, err)
		serve.Error(w, err)
		return
	}

	revision := tagOrDigest

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

	ko := sign.KeyOpts{
		FulcioURL:    fulcioclient.SigstorePublicServerURL,
		RekorURL:     "https://rekor.sigstore.dev",
		OIDCIssuer:   "https://oauth2.sigstore.dev/auth",
		OIDCClientID: "sigstore",
	}

	err = sign.SignCmd(ctx, ko, options.RegistryOptions{}, nil, []string{refstr}, "", true, "", false, false, "")

	if err != nil {
		s.error.Printf("ERROR (Cosign Keyless Sign(%q)): %v", refstr, err)
		serve.Error(w, err)
		return
	}

	// Serve new image manifest.
	img, err := s.getImage(ctx, refstr)
	if err != nil {
		s.error.Println("ERROR:", err)
		serve.Error(w, err)
		return
	}

	// TODO: verify keyless

	// Serve the manifest.
	ck := cacheKey(path, revision)
	if err := s.storage.ServeManifest(w, r, img, ck); err != nil {
		s.error.Printf("ERROR (storage.ServeManifest): %v", err)
		serve.Error(w, err)
		return
	}
}

func cacheKey(path, revision string) string {
	ck := fmt.Sprintf("%s-%s", strings.ReplaceAll(path, "/", "_"), revision)
	return fmt.Sprintf("sign-%x", ck)
}

func (s *server) getImage(ctx context.Context, image string) (v1.Image, error) {
	ref, err := name.NewTag(image, name.WeakValidation)
	if err != nil {
		return nil, err
	}
	return remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithContext(ctx))
}
