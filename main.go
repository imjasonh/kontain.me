package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/go-containerregistry/pkg/ko/build"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const bucket = "kontainme"

func main() {
	http.HandleFunc("/", handler)
	log.Println("Starting...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Println(r.Method, r.URL)
	if !strings.HasPrefix(r.URL.String(), "/v2/") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.String(), "/v2/")

	switch {
	case path == "":
		// API Version check.
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		return
	case strings.HasPrefix(path, "random/manifests/"):
		serveRandomManifest(w, r)
	case strings.HasPrefix(path, "ko/") && strings.Contains(path, "/manifests/"):
		serveKoManifest(w, r)
	case strings.HasPrefix(path, "random/blobs/"),
		strings.HasPrefix(path, "ko/blobs/"):
		serveBlob(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func serveBlob(w http.ResponseWriter, r *http.Request) {
	// Extract requested blob digest and redirect to serve it from GCS.
	// If it doesn't exist, this will return 404.
	parts := strings.Split(r.URL.Path, "/")
	digest := parts[len(parts)-1]
	url := fmt.Sprintf("https://storage.googleapis.com/%s/blobs/%s", bucket, digest)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func writeBlob(h v1.Hash, rc io.ReadCloser) error {
	ctx := context.Background() // TODO
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("NewClient: %v", err)
	}
	w := client.Bucket(bucket).Object(fmt.Sprintf("blobs/%s", h)).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	if _, err := io.Copy(w, rc); err != nil {
		return fmt.Errorf("Copy: %v", err)
	}
	if err := rc.Close(); err != nil {
		return fmt.Errorf("rc.Close: %v", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("w.Close: %v", err)
	}
	return nil
}

func getDefaultBaseImage(string) (v1.Image, error) {
	// TODO: memoize
	ref, err := name.ParseReference("gcr.io/distroless/base", name.WeakValidation)
	if err != nil {
		return nil, err
	}
	return remote.Image(ref)
}

// konta.in/ko/github.com/knative/build/cmd/controller -> ko build and serve
func serveKoManifest(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v2/ko/")
	parts := strings.Split(path, "/")
	pkg := strings.Join(parts[:len(parts)-2], "/")

	tag := parts[len(parts)-1]
	log.Printf("requested image tag :%s", tag)

	// go get the package.
	log.Printf("go get %s...", pkg)
	if err := exec.Command("go", "get", pkg).Run(); err != nil {
		log.Printf("ERROR (go get): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// TODO: Check image tag for version, resolve branches -> commits and redirect to img:<commit>
	// TODO: For requests for commit SHAs, check if it's already built and serve that instead.
	// TODO: Look for $GOPATH/$importPath/.ko.yaml and up, to base image config.

	// ko build the package.
	g, err := build.NewGo(
		build.WithBaseImages(getDefaultBaseImage),
		build.WithCreationTime(v1.Time{time.Unix(0, 0)}),
	)
	if err != nil {
		log.Printf("ERROR (build.NewGo): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !g.IsSupportedReference(pkg) {
		log.Printf("ERROR (IsSupportedReference): %s", err)
		http.Error(w, fmt.Sprintf("%q is not a supported reference", pkg), http.StatusBadRequest)
		return
	}
	img, err := g.Build(pkg)
	if err != nil {
		log.Printf("ERROR (ko build): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	serveManifest(w, img)
}

// Capture up to 99 layers of up to 99.9MB each.
var randomTagRE = regexp.MustCompile("([0-9]{1,2})x([0-9]{1,8})")

// konta.in/random:3x10mb
// konta.in/random(:latest) -> 1x10mb
func serveRandomManifest(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimPrefix(r.URL.Path, "/v2/random/manifests/")
	var num, size int64 = 1, 10000000 // 10MB

	// Captured requested num + size from tag.
	all := randomTagRE.FindStringSubmatch(tag)
	if len(all) >= 3 {
		num, _ = strconv.ParseInt(all[1], 10, 64)
		size, _ = strconv.ParseInt(all[2], 10, 64)
	}
	log.Printf("generating random image with %d layers of %d bytes", num, size)

	// Generate a random image.
	img, err := random.Image(size, num)
	if err != nil {
		log.Printf("ERROR (random.Image): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	serveManifest(w, img)
}

// serveManifest writes config and layer blobs for the image, then serves the
// manifest contents pointing to those blobs.
func serveManifest(w http.ResponseWriter, img v1.Image) {
	// Write config blob for later serving.
	ch, err := img.ConfigName()
	if err != nil {
		log.Printf("ERROR (serveManifest ConfigName): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cb, err := img.RawConfigFile()
	if err != nil {
		log.Printf("ERROR (serveManifest RawConfigFile): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("writing config blob %q", ch)
	if err := writeBlob(ch, ioutil.NopCloser(bytes.NewReader(cb))); err != nil {
		log.Printf("ERROR (serveManifest writeBlob): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Write layer blobs for later serving.
	layers, err := img.Layers()
	if err != nil {
		log.Printf("ERROR (serveManifest Layers): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, l := range layers {
		rc, err := l.Compressed()
		if err != nil {
			log.Printf("ERROR (serveManifest l.Compressed): %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lh, err := l.Digest()
		if err != nil {
			log.Printf("ERROR (serveManifest l.Digest): %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("writing layer blob %q", lh)
		if err := writeBlob(lh, rc); err != nil {
			log.Printf("ERROR (serveManifest writeBlob): %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Serve the manifest.
	b, err := img.RawManifest()
	if err != nil {
		log.Printf("ERROR (serveManifest RawManifest): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mt, err := img.MediaType()
	if err != nil {
		log.Printf("ERROR (serveManifest MediaType): %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", string(mt))
	if _, err := io.Copy(w, bytes.NewReader(b)); err != nil {
		log.Printf("ERROR (serveManifest Copy): %s", err)
		return
	}
	fmt.Printf("Served manifest: %s", string(b))
}
