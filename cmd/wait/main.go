package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/imjasonh/delay/pkg/delay"
	"github.com/imjasonh/kontain.me/pkg/serve"
)

const queueName = "wait-queue"

func main() {
	delay.Init()
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
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/master/cmd/wait", http.StatusSeeOther))

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
		s.serveWaitManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

func cacheKey(name string) string {
	ck := []byte(fmt.Sprintf("wait-%s", strings.ReplaceAll(name, "/", "_")))
	return fmt.Sprintf("%x", md5.Sum(ck))
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

	// The image has already been built; serve it.
	ck := cacheKey(name)
	if err := s.storage.BlobExists(ctx, ck); err == nil {
		s.info.Printf("blob %q exists, serving", ck)
		serve.Blob(w, r, ck)
		return
	}

	// If a placeholder exists, a wait is ongoing; serve the placeholder
	// contents.
	phn := fmt.Sprintf("placeholder-%s", ck)
	if err := s.storage.BlobExists(ctx, phn); err == nil {
		s.info.Printf("placeholder %q exists", phn)
		serve.Error(w, fmt.Errorf("waiting for image..."))
		return
	}

	// No cached image or placeholder exists; enqueue a new task.
	tag := strings.TrimPrefix(r.URL.Path, fmt.Sprintf("/v2/%s/manifests/", name))
	if tag == "latest" {
		tag = "10s"
	}
	dur, err := time.ParseDuration(tag)
	if err != nil {
		s.error.Println(err)
		serve.Error(w, err)
		return
	}
	if dur > time.Hour {
		err := fmt.Errorf("duration > 1h (%s)", dur)
		s.error.Println(err)
		serve.Error(w, err)
		return
	}
	s.info.Printf("generating random image %q in %s", ck, dur)

	// Enqueue the task for later.
	if err := laterFunc.Call(ctx, r, queueName,
		delay.WithArgs(ck),
		delay.WithDelay(dur)); err != nil {
		s.error.Printf("ERROR (laterFunc.Call): %v", err)
		serve.Error(w, err)
		return
	}

	// Write the placeholder object.
	if err := s.storage.WriteObject(ctx, phn, fmt.Sprintf("serving image at %s", time.Now().Add(dur))); err != nil {
		s.error.Printf("ERROR (storage.WriteObject): %v", err)
		serve.Error(w, err)
		return
	}

	serve.Error(w, fmt.Errorf("enqueued task to generate image in %s", dur))
}

const size = 100
const num = 10

var laterFunc = delay.Func("later", func(ctx context.Context, ck string) error {
	log.Println("generating random image for cache key %q", ck)
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
