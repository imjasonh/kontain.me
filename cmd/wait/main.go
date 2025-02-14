package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/chainguard-dev/clog/gcp"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/imjasonh/delay/pkg/delay"
	"github.com/imjasonh/kontain.me/pkg/serve"
)

const queueName = "wait-queue"

func main() {
	ctx := context.Background()
	st, err := serve.NewStorage(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "serve.NewStorage", "err", err)
		os.Exit(1)
	}
	http.Handle("/v2/", gcp.WithCloudTraceContext(&server{storage: st}))
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/random", http.StatusSeeOther))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		slog.InfoContext(ctx, "Defaulting port", "port", port)
	}
	slog.InfoContext(ctx, "Listening...", "port", port)
	slog.ErrorContext(ctx, "ListenAndServe", "err", http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

type server struct{ storage *serve.Storage }

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		s.serveWaitManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

func cacheKey(name string) string {
	ck := []byte(strings.ReplaceAll(name, "/", "_"))
	return fmt.Sprintf("wait-%x", md5.Sum(ck))
}

// wait.kontain.me/(name):5s -> enqueue task to generate random manifest in 5s
// - latest defaults to 10s
// if manifest for name exists, serve it.
// if a placeholder exists, a wait is ongoing.
func (s *server) serveWaitManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	parts := strings.Split(path, "/")
	name := strings.Join(parts[:len(parts)-2], "/")

	// If request is for image by digest, try to serve it from GCS.
	tagOrDigest := parts[len(parts)-1]
	if strings.HasPrefix(tagOrDigest, "sha256:") {
		desc, err := s.storage.BlobExists(ctx, tagOrDigest)
		if err != nil {
			slog.ErrorContext(ctx, "storage.BlobExists", "err", err)
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

	// The image has already been built; serve it.
	ck := cacheKey(name)
	if _, err := s.storage.BlobExists(ctx, ck); err == nil {
		slog.InfoContext(ctx, "blob exists", "ck", ck)
		serve.Blob(w, r, ck)
		return
	}

	// If a placeholder exists, a wait is ongoing; serve the placeholder
	// contents.
	phn := fmt.Sprintf("placeholder-%s", ck)
	if _, err := s.storage.BlobExists(ctx, phn); err == nil {
		slog.InfoContext(ctx, "placeholder exists", "phn", phn)
		serve.Error(w, fmt.Errorf("waiting for image..."))
		return
	}

	// No cached image or placeholder exists; enqueue a new task.
	tag := tagOrDigest
	if tag == "latest" {
		tag = "10s"
	}
	dur, err := time.ParseDuration(tag)
	if err != nil {
		slog.ErrorContext(ctx, "time.ParseDuration", "tag", tag, "err", err)
		serve.Error(w, err)
		return
	}
	if dur > time.Hour {
		err := fmt.Errorf("duration > 1h (%s)", dur)
		slog.ErrorContext(ctx, "duration > 1h", "tag", tag, "err", err)
		serve.Error(w, err)
		return
	}
	slog.InfoContext(ctx, "generating random image", "ck", ck, "dur", dur)

	// Enqueue the task for later.
	if err := laterFunc.Call(ctx, r, queueName,
		delay.WithArgs(ck),
		delay.WithDelay(dur)); err != nil {
		slog.ErrorContext(ctx, "laterFunc.Call", "err", err)
		serve.Error(w, err)
		return
	}

	// Write the placeholder object.
	if err := s.storage.WriteObject(ctx, phn, fmt.Sprintf("serving image at %s", time.Now().Add(dur))); err != nil {
		slog.ErrorContext(ctx, "storage.WriteObject", "err", err)
		serve.Error(w, err)
		return
	}

	serve.Error(w, fmt.Errorf("enqueued task to generate image in %s", dur))
}

const size = 100
const num = 10

var laterFunc = delay.Func("later", func(ctx context.Context, ck string) error {
	log.Printf("generating random image for cache key %q", ck)
	img, err := random.Image(size, num)
	if err != nil {
		return err
	}

	s, err := serve.NewStorage(ctx)
	if err != nil {
		return err
	}
	return s.WriteImage(ctx, img, ck)
})
