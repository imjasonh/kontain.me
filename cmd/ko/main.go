package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/ko/pkg/build"
	"github.com/imjasonh/gcpslog"
	"github.com/imjasonh/kontain.me/pkg/serve"
	"golang.org/x/mod/module"
	"golang.org/x/mod/zip"
	yaml "gopkg.in/yaml.v2"
)

func main() {
	ctx := context.Background()
	st, err := serve.NewStorage(ctx)
	if err != nil {
		slog.Error("serve.NewStorage", "err", err)
		os.Exit(1)
	}
	http.Handle("/v2/", gcpslog.WithCloudTraceContext(&server{storage: st}))
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/ko", http.StatusSeeOther))

	slog.Info("Starting...")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		slog.Info("Defaulting port", "port", port)
	}
	slog.Info("Listening", "port", port)
	slog.Error("ListenAndServe", "err", http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

type server struct{ storage *serve.Storage }

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slog.Info("handler", "method", r.Method, "url", r.URL)
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
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// ko.kontain.me/github.com/knative/build/cmd/controller -> ko build and serve
func (s *server) serveKoManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	path = strings.TrimPrefix(path, "ko/") // To handle legacy behavior.
	parts := strings.Split(path, "/")
	ip := strings.Join(parts[:len(parts)-2], "/")

	// "go get" the package
	tagOrDigest := parts[len(parts)-1]

	// If request is for image by digest, try to serve it from GCS.
	if strings.HasPrefix(tagOrDigest, "sha256:") {
		desc, err := s.storage.BlobExists(ctx, tagOrDigest)
		if err != nil {
			slog.Error("storage.BlobExists", "err", err)
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

	// Traverse up from the importpath to find the module root, by checking
	// whether the path is a module path that returns a version.
	module, version, err := walkUp(ctx, ip, tagOrDigest)
	if err != nil {
		slog.Error("walkUp", "err", err)
		serve.Error(w, err)
		return
	}

	// Check if we've already got a manifest for this importpath + resolved version.
	ck := cacheKey(ip, version)
	if _, err := s.storage.BlobExists(ctx, ck); err == nil {
		slog.Info("serving cached manifest", "ck", ck)
		serve.Blob(w, r, ck)
		return
	}
	filepath := strings.TrimPrefix(ip, module)

	// Pull the module source from the module proxy and build it.
	br, err := s.fetchAndBuild(ctx, module, version, filepath)
	if err != nil {
		slog.Error("fetchAndBuild", "err", err)
		serve.Error(w, err)
		return
	}

	if idx, ok := br.(v1.ImageIndex); ok {
		if err := s.storage.ServeIndex(w, r, idx, ck); err != nil {
			slog.Error("storage.ServeIndex", "err", err)
			serve.Error(w, err)
		}
		return
	}
	if img, ok := br.(v1.Image); ok {
		if err := s.storage.ServeManifest(w, r, img, ck); err != nil {
			slog.Error("storage.ServeManifest", "err", err)
			serve.Error(w, err)
		}
		return
	}
	serve.Error(w, errors.New("image was not image or index"))
}

func cacheKey(importpath, version string) string {
	ck := []byte(fmt.Sprintf("%s-%s", importpath, version))
	return fmt.Sprintf("ko-%x", md5.Sum(ck))
}

const defaultBaseImage = "gcr.io/distroless/static:nonroot"

func (s *server) getBaseImage(ctx context.Context, ip string) (name.Reference, build.Result, error) {
	base := defaultBaseImage
	// Assuming we're in the root of the module directory, see if we can
	// find the .ko.yaml file.
	f, err := os.Open(".ko.yaml")
	if err == nil {
		defer f.Close()
		slog.Info("Found .ko.yaml")
		var y struct {
			DefaultBaseImage   string            `yaml:"defaultBaseImage"`
			BaseImageOverrides map[string]string `yaml:"baseImageOverrides"`
		}
		if err := yaml.NewDecoder(f).Decode(&y); err != nil {
			return nil, nil, err
		}
		if y.DefaultBaseImage != "" {
			base = y.DefaultBaseImage
		}
		if bio := y.BaseImageOverrides[ip]; bio != "" {
			base = bio
		}
	}
	slog.Info("Using base image", "base", base, "ip", ip)

	ref, err := name.ParseReference(base)
	if err != nil {
		return nil, nil, err
	}
	d, err := remote.Head(ref, remote.WithContext(ctx))
	if err != nil {
		return nil, nil, err
	}
	switch d.MediaType {
	case types.DockerManifestList, types.OCIImageIndex:
		slog.Info("Base image is index", "base", base)
		idx, err := remote.Index(ref)
		return ref, idx, err
	case types.DockerManifestSchema2, types.OCIManifestSchema1:
		slog.Info("Base image is image", "base", base)
		img, err := remote.Image(ref)
		return ref, img, err
	default:
		return nil, nil, fmt.Errorf("unknown media type: %s", d.MediaType)
	}
}

// given an importpath e.g., github.com/google/go-containerregistry/cmd/crane,
// return its go module (github.com/google/go-containerregistry) by
// sequentially checking whether the Go module proxy has version info for it.
func walkUp(ctx context.Context, importpath, version string) (string, string, error) {
	parts := strings.Split(importpath, "/")
	for i := len(parts) - 1; i > 0; i-- {
		check := strings.Join(parts[:i], "/")
		if resolved, err := getVersion(ctx, check, version); err == nil {
			return check, resolved, nil
		}
	}
	return "", "", errors.New("no module found")
}

func getVersion(ctx context.Context, mod, version string) (string, error) {
	url := fmt.Sprintf("https://proxy.golang.org/%s/@v/%s.info", mod, version)
	if version == "latest" {
		url = fmt.Sprintf("https://proxy.golang.org/%s/@latest", mod)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%d: %s", resp.StatusCode, resp.Status)
	}
	defer resp.Body.Close()
	var v module.Version
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.Version, nil
}

func (s *server) fetchAndBuild(ctx context.Context, mod, version, filepath string) (build.Result, error) {
	url := fmt.Sprintf("https://proxy.golang.org/%s/@v/%s.zip", mod, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%d %s", resp.StatusCode, resp.Status)
	}
	defer resp.Body.Close()

	// Write a temp zip file.
	tmpzip, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpzip.Name()) // Clean up the zip file.
	if _, err := io.Copy(tmpzip, resp.Body); err != nil {
		return nil, err
	}
	tmpzip.Close()

	// Create a tempdir and cd into it
	// (This is only safe because concurrency=1)
	tmpdir, err := ioutil.TempDir("", "")
	if err != nil {
		return nil, err
	}
	// Clean up the temp dir. If building is successful, we'll serve a
	// cached manifest and not need to rebuild.
	defer os.RemoveAll(tmpdir)

	// Unzip and validate the module zip file.
	if err := zip.Unzip(tmpdir, module.Version{
		Path:    mod,
		Version: version,
	}, tmpzip.Name()); err != nil {
		return nil, err
	}

	// ko build the package.
	g, err := build.NewGo(
		ctx, tmpdir,
		build.WithBaseImages(s.getBaseImage),
		build.WithPlatforms("all"),
		build.WithConfig(map[string]build.Config{
			mod + filepath: build.Config{
				// Go module proxy zips include only
				// modules.txt in vendor/, so force mod mode to
				// avoid go build errors.
				Flags: build.FlagArray{"-mod=mod"},
			},
		}),
		build.WithCreationTime(v1.Time{time.Unix(0, 0)}),
		build.WithDisabledSBOM(),
	)
	if err != nil {
		return nil, err
	}
	ip := build.StrictScheme + mod + filepath
	if err := g.IsSupportedReference(ip); err != nil {
		return nil, err
	}
	slog.Info("ko build", "ip", ip)
	return g.Build(ctx, ip)
}
