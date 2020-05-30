package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/datastore"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/imjasonh/kontain.me/pkg/run"
	"github.com/imjasonh/kontain.me/pkg/serve"
	"golang.org/x/oauth2/google"
)

var projectID = ""

func init() {
	var err error
	projectID, err = metadata.ProjectID()
	if err != nil {
		log.Fatalf("metadata.ProjectID: %v", err)
	}
}

const base = "packs/run:v3alpha2"

func main() {
	ctx := context.Background()
	ds, err := datastore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("datastore.NewClient: %v", err)
	}

	http.Handle("/v2/", &server{
		info:  log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error: log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
		ds:    ds,
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
	ds          *datastore.Client
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.info.Println("handler:", r.Method, r.URL)
	path := strings.TrimPrefix(r.URL.String(), "/v2/")

	switch {
	case path == "": // API Version check.
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	case strings.Contains(path, "/blobs/"),
		strings.Contains(path, "/manifests/sha256:"):
		// Extract requested blob digest and redirect to serve it from GCS.
		// If it doesn't exist, this will return 404.
		parts := strings.Split(r.URL.Path, "/")
		digest := parts[len(parts)-1]
		serve.Blob(w, r, digest)
	case strings.Contains(path, "/manifests/"):
		s.serveBuildpackManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

func (s *server) serveBuildpackManifest(w http.ResponseWriter, r *http.Request) {
	// Prepare workspace.
	src, layers, err := s.prepareWorkspace()
	if err != nil {
		s.error.Println("ERROR:", err)
		serve.Error(w, err)
		return
	}
	// Clean up workspace.
	defer func() {
		for _, path := range []string{
			src, layers, os.Getenv("HOME"),
		} {
			if err := os.RemoveAll(path); err != nil {
				s.error.Printf("RemoveAll(%q): %v", path, err)
			}
		}
		os.Setenv("HOME", "/home/")
	}()

	// Determine source repo and revision.
	parts := strings.Split(r.URL.Path, "/")
	repo := strings.Join(parts[2:4], "/")
	path := strings.Join(parts[4:len(parts)-2], "/")
	revision := parts[len(parts)-1]
	if revision == "latest" {
		revision = "master"
	}
	if repo == "" || repo == "buildpack" {
		repo = "googlecloudplatform/buildpack-samples"
		path = "sample-go"
	}

	// Resolve branch/tag/whatever -> SHA
	revision, err = s.resolveCommit(repo, revision)
	if err != nil {
		s.error.Println("ERROR(resolveCommit):", err)
		serve.Error(w, err)
		return
	}

	// Check whether we have a cached manfiest for this revision.
	// If we do, just serve it.
	if b := s.checkCachedManifest(revision, path); len(b) != 0 {
		w.Header().Set("Content-Type", string(types.DockerManifestSchema2)) // TODO: don't hard-code
		io.Copy(w, bytes.NewReader(b))
		return
	}

	// Fetch, detect and build source.
	image, err := s.fetchAndBuild(src, layers, repo, revision, path)
	if err != nil {
		s.error.Println("ERROR:", err)
		serve.Error(w, err)
		return
	}

	// Serve new image manifest.
	img, err := s.getImage(image)
	if err != nil {
		s.error.Println("ERROR:", err)
		serve.Error(w, err)
		return
	}

	// Cache the generated manifest.
	if b, _ := img.RawManifest(); len(b) != 0 {
		s.putCachedManifest(revision, path, b)
	}

	// Serve the manifest.
	serve.Manifest(w, r, img)
}

type cachedManifest struct {
	Manifest []byte `datastore:",noindex"`
}

func key(revision, path string) string { return fmt.Sprintf("%s:%s", revision, path) }

func (s *server) checkCachedManifest(revision, path string) []byte {
	k := datastore.NameKey("Manifests", key(revision, path), nil)
	var e cachedManifest
	ctx := context.Background() // TODO
	if err := s.ds.Get(ctx, k, &e); err == datastore.ErrNoSuchEntity {
		s.info.Printf("No cached manifest digest for revision=%q path=%q", revision, path)
	} else if err != nil {
		s.error.Printf("datastore.Get: %v", err)
	}
	return e.Manifest
}

func (s *server) putCachedManifest(revision, path string, manifest []byte) {
	k := datastore.NameKey("Manifests", key(revision, path), nil)
	e := cachedManifest{manifest}
	ctx := context.Background() // TODO
	if _, err := s.ds.Put(ctx, k, &e); err != nil {
		s.error.Printf("datastore.Put: %v", err)
	}
}

// Resolves a ref (branch, tag, PR, commit) into its SHA.
// https://developer.github.com/v3/repos/commits/#get-the-sha-1-of-a-commit-reference
func (s *server) resolveCommit(repo, ref string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/commits/%s", repo, ref)
	resp, err := http.Get(url) // TODO: cache this lookup?
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", serve.ErrNotFound
	} else if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Error resolving %q (%d): %v", url, resp.StatusCode, resp.Status)
	}
	defer resp.Body.Close()
	var r struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	s.info.Printf("Resolved %q -> %q\n", ref, r.SHA)
	return r.SHA, nil
}

func (s *server) prepareWorkspace() (string, string, error) {
	// Create tempdir to store app source.
	src, err := ioutil.TempDir("", "")
	if err != nil {
		return "", "", err
	}

	// Create layers dir.
	layers, err := ioutil.TempDir("", "")
	if err != nil {
		return "", "", err
	}

	// Create and set $HOME, which is otherwise not writable.
	home, err := ioutil.TempDir("", "")
	if err != nil {
		return "", "", err
	}
	os.Setenv("HOME", home)

	return src, layers, nil
}

func (s *server) fetchAndBuild(src, layers, repo, revision, path string) (string, error) {
	image := fmt.Sprintf("gcr.io/%s/built-%d", projectID, time.Now().Unix)
	source := fmt.Sprintf("https://github.com/%s/archive/%s.tar.gz", repo, revision)

	if resp, err := http.Head(source); err != nil {
		return "", err
	} else if resp.StatusCode == http.StatusNotFound {
		return "", serve.ErrNotFound
	} else if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HEAD %s (%d): %s", source, resp.StatusCode, resp.Status)
	}

	srcpath := filepath.Join(src, path)

	ts, err := google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("google.DefaultTokenSource: %v", err)
	}
	tok, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("credentials.Token: %v", err)
	}

	for _, cmd := range []string{
		fmt.Sprintf("chown -R %d:%d %s", os.Geteuid(), os.Getgid(), src),
		fmt.Sprintf("chown -R %d:%d %s", os.Geteuid(), os.Getgid(), layers),
		fmt.Sprintf("curl -fsSL %s | tar xz --strip-components=1 -C %s", source, src),
		fmt.Sprintf("cd %s", srcpath),
		fmt.Sprintf(`
mkdir -p ~/.docker/ && cat > ~/.docker/config.json << EOF
{
  "auths": {
    "gcr.io": {
      "username": "oauth2accesstoken",
      "password": "%s"
    }
  }
}
EOF && cat ~/.docker/config.json`, tok.AccessToken),
		fmt.Sprintf("/lifecycle/detector -app=%s -group=%s/group.toml -plan=%s/plan.toml", srcpath, layers, layers),
		fmt.Sprintf("/lifecycle/analyzer -layers=%s -group=%s/group.toml %s", layers, layers, image),
		fmt.Sprintf("/lifecycle/builder -layers=%s -app=%s -group=%s/group.toml -plan=%s/plan.toml", layers, srcpath, layers, layers),
		fmt.Sprintf("/lifecycle/exporter -layers=%s -app=%s -image=%s -group=%s/group.toml %s", layers, srcpath, base, layers, image),
	} {
		if err := run.Do(s.info.Writer(), cmd); err != nil {
			return "", fmt.Errorf("Running %q: %v", cmd, err)
		}
	}
	return image, nil
}

func (s *server) getImage(image string) (v1.Image, error) {
	ref, err := name.NewTag(image, name.WeakValidation)
	if err != nil {
		return nil, err
	}
	return remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
}
