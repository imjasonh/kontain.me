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

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
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

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		serve.Error(w, serve.ErrNotFound)
		return
	}

	switch {
	case r.URL.Path == "/v2/":
		// API Version check.
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		return
	case parts[len(parts)-2] == "blobs":
		s.handleBlobs(w, r)
	case parts[len(parts)-2] == "manifests":
		s.handleManifests(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

func reqToRef(r *http.Request) (name.Reference, error) {
	parts := strings.Split(r.URL.Path, "/")
	parts = parts[1:]
	target := parts[len(parts)-1]
	repo := strings.Join(parts[1:len(parts)-2], "/")

	if strings.Contains(target, ":") {
		return name.ParseReference(repo + "@" + target)
	}
	return name.ParseReference(repo + ":" + target)
}

func (s *server) handleBlobs(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	parts = parts[1:]
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	// Must have a path of form /v2/{name}/blobs/sha256:...
	if len(parts) < 4 {
		serve.Error(w, serve.ErrNotFound)
		return
	}

	ref, err := reqToRef(r)
	if err != nil {
		serve.Error(w, err)
		return
	}

	tr, err := transport.NewWithContext(r.Context(), ref.Context().Registry, authn.Anonymous, http.DefaultTransport, []string{ref.Scope(transport.PullScope)})
	if err != nil {
		s.error.Println(err)
		serve.Error(w, err)
		return
	}

	dig := parts[len(parts)-1]
	url := "https://" + ref.Context().RegistryStr() + "/v2/" + ref.Context().String() + "/blobs/" + dig
	s.info.Println("url:", url)
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		s.error.Println(err)
		serve.Error(w, err)
		return
	}
	res, err := tr.RoundTrip(req)
	if err != nil {
		s.error.Println(err)
		serve.Error(w, err)
		return
	}
	w.Header().Set("Location", res.Header.Get("Location"))
	w.WriteHeader(res.StatusCode)
}

func (s *server) handleManifests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ref, err := reqToRef(r)
	if err != nil {
		s.error.Println(err)
		serve.Error(w, err)
		return
	}

	desc, err := remote.Get(ref)
	if err != nil {
		s.error.Println(err)
		serve.Error(w, err)
		return
	}

	if _, ok := ref.(name.Digest); !ok {
		// Request isn't by digest; let's check it!
		// Look up ref in rekor log.
		cur := desc.Digest
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
	}

	w.Header().Set("Docker-Content-Digest", desc.Digest.String())
	w.Header().Set("Content-Type", string(desc.MediaType))
	w.Header().Set("Content-Length", strconv.Itoa(int(desc.Size)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		io.Copy(w, bytes.NewReader(desc.Manifest))
	}
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
