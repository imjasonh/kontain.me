package serve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"cloud.google.com/go/storage"
	"github.com/google/go-containerregistry/pkg/v1"
	"google.golang.org/api/googleapi"
)

const bucket = "kontainme"

func Blob(w http.ResponseWriter, r *http.Request, digest string) {
	url := fmt.Sprintf("https://storage.googleapis.com/%s/blobs/%s", bucket, digest)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func writeBlob(h v1.Hash, rc io.ReadCloser, contentType string) error {
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
	w.ObjectAttrs.ContentType = contentType
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
	log.Printf("Wrote blob %s", h.String())
	return nil
}

// ServeManifest writes config and layer blobs for the image, then serves the
// manifest contents pointing to those blobs.
func Manifest(w http.ResponseWriter, r *http.Request, img v1.Image) {
	// Write config blob for later serving.
	ch, err := img.ConfigName()
	if err != nil {
		log.Printf("ERROR (serveManifest ConfigName): %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cb, err := img.RawConfigFile()
	if err != nil {
		log.Printf("ERROR (serveManifest RawConfigFile): %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("writing config blob %q", ch)
	if err := writeBlob(ch, ioutil.NopCloser(bytes.NewReader(cb)), ""); err != nil {
		log.Printf("ERROR (serveManifest writeBlob): %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Write layer blobs for later serving.
	layers, err := img.Layers()
	if err != nil {
		log.Printf("ERROR (serveManifest Layers): %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, l := range layers {
		rc, err := l.Compressed()
		if err != nil {
			log.Printf("ERROR (serveManifest l.Compressed): %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lh, err := l.Digest()
		if err != nil {
			log.Printf("ERROR (serveManifest l.Digest): %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("writing layer blob %q", lh)
		if err := writeBlob(lh, rc, ""); err != nil {
			log.Printf("ERROR (serveManifest writeBlob): %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Write the manifest as a blob.
	b, err := img.RawManifest()
	if err != nil {
		log.Printf("ERROR (serveManifest RawManifest): %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mt, err := img.MediaType()
	if err != nil {
		log.Printf("ERROR (serveManifest MediaType): %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	digest, err := img.Digest()
	if err != nil {
		log.Printf("ERROR (serveManifest Digest): %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writeBlob(digest, ioutil.NopCloser(bytes.NewReader(b)), string(mt)); err != nil {
		log.Printf("ERROR (serveManifest writeBlob): %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Redirect to manifest blob.
	Blob(w, r, digest.String())
	fmt.Printf("Served manifest: %s", string(b))
}
