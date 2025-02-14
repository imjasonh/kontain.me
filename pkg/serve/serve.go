package serve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/storage"
	"github.com/chainguard-dev/terraform-infra-common/pkg/httpmetrics"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
)

func init() {
	http.DefaultTransport = httpmetrics.Transport
	go httpmetrics.ServeMetrics()
}

var bucket = os.Getenv("BUCKET")

func Blob(w http.ResponseWriter, r *http.Request, name string) {
	url := fmt.Sprintf("https://storage.googleapis.com/%s/blobs/%s", bucket, name)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

type Storage struct {
	client *storage.Client
}

func NewStorage(ctx context.Context) (*Storage, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("NewClient: %v", err)
	}
	return &Storage{client}, nil
}

func (s *Storage) BlobExists(ctx context.Context, name string) (v1.Descriptor, error) {
	obj, err := s.client.Bucket(bucket).Object(fmt.Sprintf("blobs/%s", name)).Attrs(ctx)
	if err != nil {
		return v1.Descriptor{}, err
	}
	var h v1.Hash
	if d := obj.Metadata["Docker-Content-Digest"]; d != "" {
		h, err = v1.NewHash(d)
		if err != nil {
			return v1.Descriptor{}, err
		}
	}

	return v1.Descriptor{
		Digest:    h,
		MediaType: types.MediaType(obj.ContentType),
		Size:      obj.Size,
	}, nil
}

func (s *Storage) WriteObject(ctx context.Context, name, contents string) error {
	w := s.client.Bucket(bucket).Object(fmt.Sprintf("blobs/%s", name)).
		If(storage.Conditions{DoesNotExist: true}).
		NewWriter(ctx)
	if _, err := fmt.Fprintln(w, contents); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("fmt.Fprintln: %v", err)
	}
	if err := w.Close(); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("w.Close: %v", err)
	}
	return nil
}

func (s *Storage) writeBlob(ctx context.Context, name string, h v1.Hash, rc io.ReadCloser, contentType string) error {
	start := time.Now()
	defer func() { log.Printf("writeBlob(%q) took %s", name, time.Since(start)) }()

	// The DoesNotExist precondition can be hit when writing or flushing
	// data, which can happen any of three places. Anywhere it happens,
	// just ignore the error since that means the blob already exists.
	w := s.client.Bucket(bucket).Object(fmt.Sprintf("blobs/%s", name)).
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
	return nil
}

// ServeIndex writes manifest, config and layer blobs for each image in the
// index, then writes and redirects to the index manifest contents pointing to
// those blobs.
func (s *Storage) ServeIndex(w http.ResponseWriter, r *http.Request, idx v1.ImageIndex, also ...string) error {
	ctx := r.Context()
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
			return s.WriteImage(ctx, img)
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
	if err := s.writeBlob(ctx, digest.String(), digest, io.NopCloser(bytes.NewReader(b)), string(mt)); err != nil {
		return err
	}

	for _, a := range also {
		a := a
		g.Go(func() error {
			return s.writeBlob(ctx, a, digest, io.NopCloser(bytes.NewReader(b)), string(mt))
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// If it's just a HEAD request, serve that.
	if r.Method == http.MethodHead {
		s, err := idx.Size()
		if err != nil {
			return err
		}
		w.Header().Set("Docker-Content-Digest", digest.String())
		w.Header().Set("Content-Type", string(mt))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", s))
		return nil
	}

	// Redirect to manifest blob.
	Blob(w, r, digest.String())
	return nil
}

// WriteImage writes the layer blobs, config blob and manifest.
func (s *Storage) WriteImage(ctx context.Context, img v1.Image, also ...string) error {
	// Write config blob for later serving.
	ch, err := img.ConfigName()
	if err != nil {
		return err
	}
	cb, err := img.RawConfigFile()
	if err != nil {
		return err
	}
	if err := s.writeBlob(ctx, ch.String(), ch, io.NopCloser(bytes.NewReader(cb)), "application/json"); err != nil {
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
			mt, err := l.MediaType()
			if err != nil {
				return err
			}
			return s.writeBlob(ctx, lh.String(), lh, rc, string(mt))
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
	if err := s.writeBlob(ctx, digest.String(), digest, io.NopCloser(bytes.NewReader(b)), string(mt)); err != nil {
		return err
	}
	for _, a := range also {
		a := a
		g.Go(func() error {
			return s.writeBlob(ctx, a, digest, io.NopCloser(bytes.NewReader(b)), string(mt))
		})
	}
	return g.Wait()
}

// ServeManifest writes config and layer blobs for the image, then writes and
// redirects to the image manifest contents pointing to those blobs.
func (s *Storage) ServeManifest(w http.ResponseWriter, r *http.Request, img v1.Image, also ...string) error {
	ctx := r.Context()
	if err := s.WriteImage(ctx, img, also...); err != nil {
		return err
	}

	digest, err := img.Digest()
	if err != nil {
		return err
	}

	// If it's just a HEAD request, serve that.
	if r.Method == http.MethodHead {
		mt, err := img.MediaType()
		if err != nil {
			return err
		}
		s, err := img.Size()
		if err != nil {
			return err
		}
		w.Header().Set("Docker-Content-Digest", digest.String())
		w.Header().Set("Content-Type", string(mt))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", s))
		return nil
	}

	// Redirect to manifest blob.
	Blob(w, r, digest.String())
	return nil
}
