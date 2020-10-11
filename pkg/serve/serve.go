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
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
)

const bucket = "kontainme"

func Blob(w http.ResponseWriter, r *http.Request, digest string) {
	url := fmt.Sprintf("https://storage.googleapis.com/%s/blobs/%s", bucket, digest)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func BlobExists(h v1.Hash) error {
	ctx := context.Background() // TODO
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("NewClient: %v", err)
	}
	_, err = client.Bucket(bucket).Object(fmt.Sprintf("blobs/%s", h)).Attrs(ctx)
	return err
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
	w.Metadata = map[string]string{"Docker-Content-Digest": h.String()}
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

// Index writes manifest, config and layer blobs for each image in the index,
// then writes and redirects to the index manifest contents pointing to those
// blobs.
func Index(w http.ResponseWriter, r *http.Request, idx v1.ImageIndex) error {
	im, err := idx.IndexManifest()
	if err != nil {
		return err
	}
	var g errgroup.Group
	for _, m := range im.Manifests {
		m := m
		g.Go(func() error {
			img, err := idx.Image(m.Digest)
			if err != nil {
				return err
			}
			return writeManifest(img)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Write the manifest as a blob.
	b, err := idx.RawManifest()
	if err != nil {
		return err
	}
	mt, err := idx.MediaType()
	if err != nil {
		return err
	}
	digest, err := idx.Digest()
	if err != nil {
		return err
	}
	if err := writeBlob(digest, ioutil.NopCloser(bytes.NewReader(b)), string(mt)); err != nil {
		return err
	}

	// Redirect to manifest blob.
	Blob(w, r, digest.String())
	return nil
}

func writeManifest(img v1.Image) error {
	// Write config blob for later serving.
	ch, err := img.ConfigName()
	if err != nil {
		return err
	}
	cb, err := img.RawConfigFile()
	if err != nil {
		return err
	}
	log.Printf("writing config blob %q", ch)
	if err := writeBlob(ch, ioutil.NopCloser(bytes.NewReader(cb)), ""); err != nil {
		return err
	}

	// Write layer blobs for later serving.
	layers, err := img.Layers()
	if err != nil {
		return err
	}
	var g errgroup.Group
	for _, l := range layers {
		l := l
		g.Go(func() error {
			rc, err := l.Compressed()
			if err != nil {
				return err
			}
			lh, err := l.Digest()
			if err != nil {
				return err
			}
			log.Printf("writing layer blob %q", lh)
			return writeBlob(lh, rc, "")
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Write the manifest as a blob.
	b, err := img.RawManifest()
	if err != nil {
		return err
	}
	mt, err := img.MediaType()
	if err != nil {
		return err
	}
	digest, err := img.Digest()
	if err != nil {
		return err
	}
	if err := writeBlob(digest, ioutil.NopCloser(bytes.NewReader(b)), string(mt)); err != nil {
		return err
	}
	return nil
}

// Manifest writes config and layer blobs for the image, then writes and
// redirects to the image manifest contents pointing to those blobs.
func Manifest(w http.ResponseWriter, r *http.Request, img v1.Image) error {
	if err := writeManifest(img); err != nil {
		return err
	}

	digest, err := img.Digest()
	if err != nil {
		return err
	}

	// Redirect to manifest blob.
	Blob(w, r, digest.String())
	return nil
}
