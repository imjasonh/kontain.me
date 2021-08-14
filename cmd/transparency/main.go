package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/imjasonh/kontain.me/pkg/serve"
)

func main() {
	http.Handle("/v2/", &server{
		info:  log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error: log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
	})
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/flatten", http.StatusSeeOther))

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
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.info.Println("handler:", r.Method, r.URL)
	path := strings.TrimPrefix(r.URL.String(), "/v2/")
	parts := strings.Split(path, "/")

	switch {
	case r.URL.Path == "/v2/":
		// API Version check.
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		return
	case parts[len(parts)-2] == "blobs":
		s.redirect(w, r)
	case parts[len(parts)-2] == "manifests":
		s.checkManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

// path /v2/gcr.io/foo/bar/baz/blobs/sha -> https://gcr.io/v2/foo/bar/baz/blobs/sha
// path /v2/ubuntu/blobs/sha -> https://index.docker.io/v2/library/ubuntu/blobs/sha
func (s *server) redirect(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		serve.Error(w, serve.ErrNotFound)
		return
	}
	repo := strings.Join(parts[2:len(parts)-2], "/")
	ref, err := name.ParseReference(repo)
	if err != nil {
		s.error.Printf("ERROR (ParseReference(%q)): %v", repo, err)
		serve.Error(w, err)
		return
	}

	dig := parts[len(parts)-1]
	url := "https://" + ref.Context().RegistryStr() + "/v2/" + ref.Context().String() + "/blobs/" + dig
	s.info.Println("redirecting to", url)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// /v2/gcr.io/foo/bar/baz/manifests/tag-name -> gcr.io/foo/bar/baz:tag-name
func (s *server) checkManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		serve.Error(w, serve.ErrNotFound)
		return
	}
	// Parse and canonicalize to handle "ubuntu" -> "index.docker.io/library/ubuntu"
	last := parts[len(parts)-1]
	sep := ":"
	if strings.Contains(last, ":") {
		sep = "@"
	}
	refstr := strings.Join(parts[2:len(parts)-2], "/") + sep + last
	s.info.Println("request for ref", refstr)
	ref, err := name.ParseReference(refstr)
	if err != nil {
		s.error.Printf("ERROR (ParseReference(%q)): %v", refstr, err)
		serve.Error(w, err)
		return
	}

	// Look up the ref's current digest.
	desc, err := remote.Get(ref, remote.WithContext(ctx))
	if err != nil {
		s.error.Printf("ERROR (remote.Get(%q)): %v", ref, err)
		serve.Error(w, err)
		return
	}
	cur := desc.Digest
	s.info.Printf("current: %s -> %s", ref, cur)

	// If request is by digest, serve it directly.
	if _, ok := ref.(name.Digest); ok {
		w.Header().Set("Docker-Content-Digest", desc.Digest.String())
		w.Header().Set("Content-Type", string(desc.MediaType))
		w.Header().Set("Content-Length", strconv.Itoa(int(desc.Size)))
		w.WriteHeader(http.StatusOK)
		io.Copy(w, bytes.NewReader(desc.Manifest))
		return
	}

	// Look up ref in rekor log.
	got, err := s.lookup(ctx, ref)
	if err == errRekordNotFound {
		// Ref wasn't found, record it.
		if err := s.record(ctx, ref, cur); err != nil {
			s.error.Printf("ERROR (record): %v", ref, err)
			serve.Error(w, err)
			return
		}
	} else if err != nil {
		// Lookup failed!
		s.error.Printf("ERROR (lookup): %v", ref, err)
		serve.Error(w, err)
		return
	} else {
		s.info.Printf("rekor: %s -> %s", ref, got)
		if got != cur {
			s.error.Printf("ERROR (mismatch): got %q, want %q", got, cur)
			serve.Error(w, fmt.Errorf("rekor digest mismatch: got %q, want %q", got, cur))
			return
		}
		// Otherwise, proceed and redirect.
	}

	// Write the manifest out.
	w.Header().Set("Docker-Content-Digest", desc.Digest.String())
	w.Header().Set("Content-Type", string(desc.MediaType))
	w.Header().Set("Content-Length", strconv.Itoa(int(desc.Size)))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, bytes.NewReader(desc.Manifest))
}

var errRekordNotFound = errors.New("record not found in rekor")

func (s *server) lookup(ctx context.Context, ref name.Reference) (v1.Hash, error) {
	// TODO implement
	return v1.Hash{}, errRekordNotFound
}

func (s *server) record(ctx context.Context, ref name.Reference, digest v1.Hash) error {
	// TODO implement
	return nil
}
