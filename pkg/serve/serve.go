package serve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"cloud.google.com/go/storage"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
)

var bucket = os.Getenv("BUCKET")

func Blob(w http.ResponseWriter, r *http.Request, name string) {
	url := fmt.Sprintf("https://storage.googleapis.com/%s/blobs/%s", bucket, name)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func BlobExists(name string) error {
	ctx := context.Background() // TODO
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("NewClient: %v", err)
	}
	_, err = client.Bucket(bucket).Object(fmt.Sprintf("blobs/%s", name)).Attrs(ctx)
	return err
}

func writeBlob(name string, h v1.Hash, rc io.ReadCloser, contentType string) error {
	ctx := context.Background() // TODO
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("NewClient: %v", err)
	}
	// The DoesNotExist precondition can be hit when writing or flushing
	// data, which can happen any of three places. Anywhere it happens,
	// just ignore the error since that means the blob already exists.
	w := client.Bucket(bucket).Object(fmt.Sprintf("blobs/%s", name)).
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
	log.Printf("wrote blob %s", name)
	return nil
}

// Index writes manifest, config and layer blobs for each image in the index,
// then writes and redirects to the index manifest contents pointing to those
// blobs.
func Index(w http.ResponseWriter, r *http.Request, idx v1.ImageIndex, also ...string) error {
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
			return writeImage(img)
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
	log.Printf("writing index manifest %q", digest)
	if err := writeBlob(digest.String(), digest, ioutil.NopCloser(bytes.NewReader(b)), string(mt)); err != nil {
		return err
	}

	for _, a := range also {
		a := a
		g.Go(func() error {
			log.Printf("writing index manifest %q also to %q", digest, a)
			return writeBlob(a, digest, ioutil.NopCloser(bytes.NewReader(b)), string(mt))
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Redirect to manifest blob.
	Blob(w, r, digest.String())
	return nil
}

// writeImage writes the layer blobs, config blob and manifest.
func writeImage(img v1.Image, also ...string) error {
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
	if err := writeBlob(ch.String(), ch, ioutil.NopCloser(bytes.NewReader(cb)), ""); err != nil {
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
			return writeBlob(lh.String(), lh, rc, "")
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
	log.Printf("writing image manifest %q", digest)
	if err := writeBlob(digest.String(), digest, ioutil.NopCloser(bytes.NewReader(b)), string(mt)); err != nil {
		return err
	}
	for _, a := range also {
		a := a
		g.Go(func() error {
			log.Printf("writing image manifest %q also to %q", digest, a)
			return writeBlob(a, digest, ioutil.NopCloser(bytes.NewReader(b)), string(mt))
		})
	}
	return g.Wait()
	return nil
}

// Manifest writes config and layer blobs for the image, then writes and
// redirects to the image manifest contents pointing to those blobs.
func Manifest(w http.ResponseWriter, r *http.Request, img v1.Image, also ...string) error {
	if err := writeImage(img, also...); err != nil {
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
