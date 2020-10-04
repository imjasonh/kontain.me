// Copyright 2018 Google LLC All Rights Reserved.
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
	"context"
	"errors"
	"fmt"
	gobuild "go/build"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/ko/pkg/build"
	"github.com/imjasonh/kontain.me/pkg/run"
	"github.com/imjasonh/kontain.me/pkg/serve"
	yaml "gopkg.in/yaml.v2"
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
		s.serveKoManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

// konta.in/ko/github.com/knative/build/cmd/controller -> ko build and serve
func (s *server) serveKoManifest(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v2/ko/")
	parts := strings.Split(path, "/")
	ip := strings.Join(parts[:len(parts)-2], "/")

	tag := parts[len(parts)-1]
	s.info.Printf("requested image tag :%s", tag)

	// go get the package.
	s.info.Printf("go get %s...", ip)
	if err := run.Do(s.info.Writer(), fmt.Sprintf("go get %s", ip)); err != nil {
		s.error.Printf("ERROR (go get): %s", err)
		serve.Error(w, err)
		return
	}

	// TODO: Check image tag for version, resolve branches -> commits and redirect to img:<commit>
	// TODO: For requests for commit SHAs, check if it's already built and serve that instead.

	// ko build the package.
	g, err := build.NewGo(
		build.WithBaseImages(s.getBaseImage),
		build.WithCreationTime(v1.Time{time.Unix(0, 0)}),
	)
	if err != nil {
		s.error.Printf("ERROR (build.NewGo): %s", err)
		serve.Error(w, serve.ErrInvalid)
		return
	}
	ip = build.StrictScheme + ip
	if !g.IsSupportedReference(ip) {
		s.error.Printf("ERROR (IsSupportedReference): %s", err)
		serve.Error(w, serve.ErrInvalid)
		return
	}
	s.info.Printf("ko build %s...", ip)
	br, err := g.Build(context.Background(), ip)
	if err != nil {
		s.error.Printf("ERROR (ko build): %s", err)
		serve.Error(w, serve.ErrInvalid)
		return
	}
	if idx, ok := br.(v1.ImageIndex); ok {
		if err := serve.Index(w, r, idx); err != nil {
			s.error.Printf("ERROR (serve.Index): %v", err)
			serve.Error(w, err)
		}
		return
	}
	if img, ok := br.(v1.Image); ok {
		if err := serve.Manifest(w, r, img); err != nil {
			s.error.Printf("ERROR (serve.Manifest): %v", err)
			serve.Error(w, err)
		}
		return
	}
	serve.Error(w, errors.New("image was not image or index"))
}

const defaultBaseImage = "gcr.io/distroless/static:nonroot"

func (s *server) getBaseImage(ip string) (build.Result, error) {
	ip = strings.TrimPrefix(ip, build.StrictScheme)
	base, err := s.getKoYAMLBaseImage(ip)
	if err != nil {
		return nil, err
	}
	if base == "" {
		base = defaultBaseImage
	}
	s.info.Printf("Using base image %q for %s", base, ip)

	ref, err := name.ParseReference(base)
	if err != nil {
		return nil, err
	}
	d, err := remote.Head(ref)
	if err != nil {
		return nil, err
	}
	switch d.MediaType {
	case types.DockerManifestList:
		s.info.Printf("Base image %q is manifest list", base)
		return remote.Index(ref)
	case types.DockerManifestSchema2:
		s.info.Printf("Base image %q is image", base)
		return remote.Image(ref)
	default:
		return nil, fmt.Errorf("unknown media type: %s", d.MediaType)
	}
}

func (s *server) getKoYAMLBaseImage(ip string) (string, error) {
	gopath := gobuild.Default.GOPATH
	orig := ip
	for ; ip != "."; ip = filepath.Dir(ip) {
		fp := filepath.Join(gopath, "src", ip, ".ko.yaml")
		f, err := os.Open(fp)
		if os.IsNotExist(err) {
			// Keep walking.
			continue
		} else if err != nil {
			return "", err
		}
		defer f.Close()

		s.info.Printf("Found .ko.yaml at %q", fp)

		// .ko.yaml was found, let's try to parse it.
		var y struct {
			DefaultBaseImage   string            `yaml:"defaultBaseImage"`
			BaseImageOverrides map[string]string `yaml:"baseImageOverrides"`
		}
		if err := yaml.NewDecoder(f).Decode(&y); err != nil {
			return "", err
		}
		if bio, ok := y.BaseImageOverrides[orig]; ok {
			return bio, nil
		}
		return y.DefaultBaseImage, nil
	}
	// No .ko.yaml found walking up to repo root.
	return "", nil
}
