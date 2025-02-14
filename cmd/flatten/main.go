package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/chainguard-dev/clog/gcp"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/imjasonh/kontain.me/pkg/serve"
	"golang.org/x/sync/errgroup"
)

func main() {
	ctx := context.Background()
	st, err := serve.NewStorage(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "serve.NewStorage", "err", err)
		os.Exit(1)
	}
	http.Handle("/v2/", gcp.WithCloudTraceContext(&server{storage: st}))
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/flatten", http.StatusSeeOther))

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
		s.serveFlattenManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

var acceptableMediaTypes = map[types.MediaType]bool{
	types.DockerManifestSchema2: true,
	types.DockerManifestList:    true,
	types.OCIImageIndex:         true,
	types.OCIManifestSchema1:    true,
}

func cacheKey(orig string) string { return fmt.Sprintf("flatten-%s", orig) }

// flatten.kontain.me/ubuntu -> flatten ubuntu and serve
func (s *server) serveFlattenManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	parts := strings.Split(path, "/")

	refstr := strings.Join(parts[:len(parts)-2], "/")
	tagOrDigest := parts[len(parts)-1]
	if strings.HasPrefix(tagOrDigest, "sha256:") {
		refstr += "@" + tagOrDigest
	} else {
		refstr += ":" + tagOrDigest
	}
	for strings.HasPrefix(refstr, "flatten.kontain.me/") {
		refstr = strings.TrimPrefix(refstr, "flatten.kontain.me/")
	}

	ref, err := name.ParseReference(refstr)
	if err != nil {
		slog.ErrorContext(ctx, "name.ParseReference", "ref", refstr, "err", err)
		serve.Error(w, err)
		return
	}

	var idx v1.ImageIndex
	var img v1.Image
	var ck string

	// Determine whether the ref is for an image or index.
	d, err := remote.Head(ref, remote.WithContext(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "remote.Head", "ref", refstr, "err", err)
		var h v1.Hash
		// HEAD failed, let's figure out if it was an index or image by doing GETs.
		idx, err = remote.Index(ref, remote.WithContext(ctx))
		if err != nil {
			slog.ErrorContext(ctx, "remote.Index", "ref", refstr, "err", err)
			img, err = remote.Image(ref, remote.WithContext(ctx))
			if err != nil {
				slog.ErrorContext(ctx, "remote.Image", "ref", refstr, "err", err)
				serve.Error(w, err)
				return
			}
		}

		if idx != nil {
			h, err = idx.Digest()
		} else if img != nil {
			h, err = img.Digest()
		}
		if err != nil {
			slog.ErrorContext(ctx, "Digest()", "ref", refstr, "err", err)
			serve.Error(w, err)
			return
		}

		// Check if we have a flattened manifest cached (since HEAD failed
		// before), and if so serve it directly.
		ck = cacheKey(h.String())
		if _, err := s.storage.BlobExists(ctx, ck); err == nil {
			slog.InfoContext(ctx, "serving cached manifest", "ck", ck)
			serve.Blob(w, r, ck)
			return
		}
	} else {
		if !acceptableMediaTypes[d.MediaType] {
			err = fmt.Errorf("unknown media type: %s", d.MediaType)
			slog.ErrorContext(ctx, "unknown media type", "ref", refstr, "err", err)
			serve.Error(w, err)
			return
		}

		// Check if we have a flattened manifest cached, and if so serve it
		// directly.
		ck = cacheKey(d.Digest.String())
		if _, err := s.storage.BlobExists(ctx, ck); err == nil {
			slog.InfoContext(ctx, "serving cached manifest", "ck", ck)
			serve.Blob(w, r, ck)
			return
		}

		switch d.MediaType {
		case types.OCIImageIndex, types.DockerManifestList:
			idx, err = remote.Index(ref, remote.WithContext(ctx))
			if err != nil {
				err = fmt.Errorf("remote.Index: %v", err)
				slog.ErrorContext(ctx, "remote.Index", "ref", refstr, "err", err)
				serve.Error(w, err)
				return
			}
		case types.OCIManifestSchema1, types.DockerManifestSchema2:
			img, err = remote.Image(ref, remote.WithContext(ctx))
			if err != nil {
				err = fmt.Errorf("remote.Image: %v", err)
				slog.ErrorContext(ctx, "remote.Image", "ref", refstr, "err", err)
				serve.Error(w, err)
				return
			}
		}
	}

	if idx != nil {
		fidx, err := s.flattenIndex(ctx, idx)
		if err != nil {
			serve.Error(w, err)
			return
		}

		if err := s.storage.ServeIndex(w, r, fidx, ck); err != nil {
			slog.ErrorContext(ctx, "storage.ServeIndex", "err", err)
			serve.Error(w, err)
			return
		}
		return
	}

	if img != nil {
		fimg, err := s.flatten(ctx, img)
		if err != nil {
			serve.Error(w, err)
			return
		}

		if err := s.storage.ServeManifest(w, r, fimg, ck); err != nil {
			slog.ErrorContext(ctx, "storage.ServeManifest", "err", err)
			serve.Error(w, err)
			return
		}
		return
	}

}

func (s *server) flattenIndex(ctx context.Context, idx v1.ImageIndex) (v1.ImageIndex, error) {
	im, err := idx.IndexManifest()
	if err != nil {
		slog.ErrorContext(ctx, "idx.IndexManifest", "err", err)
		return nil, err
	}
	// Flatten each image in the manifest.
	var g errgroup.Group
	adds := make([]mutate.IndexAddendum, len(im.Manifests))
	for i, m := range im.Manifests {
		i, m := i, m
		g.Go(func() error {
			img, err := idx.Image(m.Digest)
			if err != nil {
				slog.ErrorContext(ctx, "idx.Image", "err", err)
				return err
			}
			fimg, err := s.flatten(ctx, img)
			if err != nil {
				return err
			}
			m.Digest, err = fimg.Digest()
			if err != nil {
				slog.ErrorContext(ctx, "fimg.Digest", "err", err)
				return err
			}
			adds[i] = mutate.IndexAddendum{
				Add:        fimg,
				Descriptor: m,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		slog.ErrorContext(ctx, "g.Wait", "err", err)
		return nil, err
	}
	return mutate.AppendManifests(empty.Index, adds...), nil
}

func (s *server) flatten(ctx context.Context, img v1.Image) (v1.Image, error) {
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return mutate.Extract(img), nil })
	if err != nil {
		slog.ErrorContext(ctx, "tarball.LayerFromOpener", "err", err)
		return nil, err
	}
	fimg, err := mutate.AppendLayers(empty.Image, l)
	if err != nil {
		slog.ErrorContext(ctx, "mutate.AppendLayers", "err", err)
		return nil, err
	}

	// Copy over basic information from original config file.
	ocf, err := img.ConfigFile()
	if err != nil {
		slog.ErrorContext(ctx, "img.ConfigFile", "err", err)
		return nil, err
	}
	ncf, err := fimg.ConfigFile()
	if err != nil {
		slog.ErrorContext(ctx, "fimg.ConfigFile", "err", err)
		return nil, err
	}
	cf := ncf.DeepCopy()
	cf.Architecture = ocf.Architecture
	cf.OS = ocf.OS
	cf.OSVersion = ocf.OSVersion
	cf.Config = ocf.Config

	return mutate.ConfigFile(fimg, cf)
}
