package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/imjasonh/gcpslog"
	"github.com/imjasonh/kontain.me/pkg/serve"
)

func main() {
	ctx := context.Background()
	st, err := serve.NewStorage(ctx)
	if err != nil {
		slog.Error("serve.NewStorage", "err", err)
		os.Exit(1)
	}
	http.Handle("/v2/", gcpslog.WithCloudTraceContext(&server{storage: st}))
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/random", http.StatusSeeOther))

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
		s.serveRandomManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

// Capture up to 99 layers of up to 99.9MB each.
var randomTagRE = regexp.MustCompile("([0-9]{1,2})x([0-9]{1,8})")

// random.kontain.me:3x10mb
// random.kontain.me(:latest) -> 1x10mb
func (s *server) serveRandomManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tagOrDigest := strings.TrimPrefix(r.URL.Path, "/v2/manifests/")
	var num, size int64 = 1, 10000000 // 10MB

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

	// Captured requested num + size from tag.
	all := randomTagRE.FindStringSubmatch(tagOrDigest)
	if len(all) >= 3 {
		num, _ = strconv.ParseInt(all[1], 10, 64)
		size, _ = strconv.ParseInt(all[2], 10, 64)
	}
	slog.Info("generating random image", "layers", num, "size", size)

	// Generate a random image.
	img, err := random.Image(size, num)
	if err != nil {
		slog.Error("random.Image", "err", err)
		serve.Error(w, err)
		return
	}
	if err := s.storage.ServeManifest(w, r, img); err != nil {
		slog.Error("storage.ServeManifest", "err", err)
		serve.Error(w, err)
		return
	}
}
