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
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/imjasonh/kontain.me/pkg/serve"
	"golang.org/x/sync/errgroup"
)

func main() {
	http.Handle("/v2/", &server{
		info:  log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error: log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
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
	if err := serve.BlobExists(ck); err == nil {
		s.info.Println("serving cached manifest:", ck)
		serve.Blob(w, r, ck)
		return
	}

	switch d.MediaType {
	case types.DockerManifestList:
		idx, err := remote.Index(ref)
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
				l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return mutate.Extract(img), nil })
				if err != nil {
					s.error.Printf("ERROR (tarball.LayerFromOpener): %v", err)
					return err
				}
				fimg, err := mutate.AppendLayers(empty.Image, l)
				if err != nil {
					s.error.Printf("ERROR (mutate.AppendLayers): %v", err)
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
		if err := serve.Index(w, r, fidx, ck); err != nil {
			s.error.Printf("ERROR (serve.Index): %v", err)
			serve.Error(w, err)
			return
		}

	case types.DockerManifestSchema2:
		img, err := remote.Image(ref)
		if err != nil {
			s.error.Printf("ERROR (remote.Image): %v", err)
			serve.Error(w, err)
			return
		}

		l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return mutate.Extract(img), nil })
		if err != nil {
			s.error.Printf("ERROR (tarball.LayerFromOpener): %v", err)
			serve.Error(w, err)
			return
		}
		fimg, err := mutate.AppendLayers(empty.Image, l)
		if err != nil {
			s.error.Printf("ERROR (mutate.AppendLayers): %v", err)
			serve.Error(w, err)
			return
		}

		if err := serve.Manifest(w, r, fimg, ck); err != nil {
			s.error.Printf("ERROR (serve.Manifest): %v", err)
			serve.Error(w, err)
			return
		}
	default:
		err := fmt.Errorf("unknown media type: %s", d.MediaType)
		s.error.Printf("ERROR (serveFlattenManifest): %v", err)
		serve.Error(w, err)
	}
}
