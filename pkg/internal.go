package pkg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/google/go-containerregistry/pkg/v1"
	"google.golang.org/api/googleapi"
)

const bucket = "kontainme"

func Run(stdout io.Writer, command string) error {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd.Run()
}

func ServeBlob(w http.ResponseWriter, r *http.Request) {
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
	// The DoesNotExist precondition can be hit when writing or flushing
	// data, which can happen any of three places. Anywhere it happens,
	// just ignore the error since that means the blob already exists.
	w := client.Bucket(bucket).Object(fmt.Sprintf("blobs/%s", h)).
		If(storage.Conditions{DoesNotExist: true}).
		NewWriter(ctx)
	if _, err := io.Copy(w, rc); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("Copy: %v", err)
	}
	if err := rc.Close(); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("rc.Close: %v", err)
	}
	if err := w.Close(); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("w.Close: %v", err)
	}
	return nil
}

// ServeManifest writes config and layer blobs for the image, then serves the
// manifest contents pointing to those blobs.
func ServeManifest(w http.ResponseWriter, img v1.Image) {
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
