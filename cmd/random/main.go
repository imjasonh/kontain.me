package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/imjasonh/kontain.me/pkg/serve"
)

func main() {
	http.Handle("/v2/", &server{
		info:  log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error: log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
	})

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
	case strings.Contains(path, "/manifests/"):
		s.serveRandomManifest(w, r)
	case strings.Contains(path, "/blobs/"):
		serve.Blob(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

// Capture up to 99 layers of up to 99.9MB each.
var randomTagRE = regexp.MustCompile("([0-9]{1,2})x([0-9]{1,8})")

// random.kontain.me:3x10mb
// random.kontain.me(:latest) -> 1x10mb
func (s *server) serveRandomManifest(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimPrefix(r.URL.Path, "/v2/manifests/")
	var num, size int64 = 1, 10000000 // 10MB

	s.info.Println("TAG", tag)

	// Captured requested num + size from tag.
	all := randomTagRE.FindStringSubmatch(tag)
	if len(all) >= 3 {
		num, _ = strconv.ParseInt(all[1], 10, 64)
		size, _ = strconv.ParseInt(all[2], 10, 64)
	}
	s.info.Printf("generating random image with %d layers of %d bytes", num, size)

	// Generate a random image.
	img, err := random.Image(size, num)
	if err != nil {
		s.error.Printf("ERROR (random.Image): %s", err)
		serve.Error(w, err)
		return
	}
	serve.Manifest(w, img)
}
