// Copyright 2020 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/imjasonh/kontain.me/pkg/serve"
)

func main() {
	http.Handle("/v2/", &server{
		info:  log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error: log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
	})
	http.Handle("/", http.RedirectHandler("https://kontain.me", http.StatusMovedPermanently))

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
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	parts := strings.Split(path, "/")
	ref, err := name.ParseReference(strings.Join(parts[:len(parts)-2], "/"))
	if err != nil {
		s.error.Printf("ERROR (ParseReference): %v", err)
		serve.Error(w, err)
		return
	}

	// Get the original image's digest, and check if we have that manifest
	// blob.
	d, err := remote.Head(ref)
	if err != nil {
		s.error.Printf("ERROR (remote.Head): %v", err)
		serve.Error(w, err)
		return
	}
	if err := serve.BlobExists(d.Digest); err != nil {
		s.info.Printf("INFO (serve.BlobExists(%q)): %v", d, err)

		// Blob doesn't exist yet. Try to get the image manifest+layers
		// and cache them.
		switch d.MediaType {
		case types.DockerManifestList:
			// If the image is a manifest list, fetch and mirror
			// the image index.
			idx, err := remote.Index(ref)
			if err != nil {
				s.error.Printf("ERROR (remote.Index): %v", err)
				serve.Error(w, err)
				return
			}
			if err := serve.Index(w, r, idx); err != nil {
				s.error.Printf("ERROR (serve.Index): %v", err)
				serve.Error(w, err)
				return
			}
		case types.DockerManifestSchema2:
			// If it's a simple image, fetch and mirror its
			// manifest.
			img, err := remote.Image(ref)
			if err != nil {
				s.error.Printf("ERROR (remote.Image): %v", err)
				serve.Error(w, err)
				return
			}
			if err := serve.Manifest(w, r, img); err != nil {
				s.error.Printf("ERROR (serve.Index): %v", err)
				serve.Error(w, err)
				return
			}
		}

	} else {
		serve.Blob(w, r, d.Digest.String())
	}
}
