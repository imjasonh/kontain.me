package main

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	gauthn "github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-github/v32/github"
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

var commitRE = regexp.MustCompile("[a-f0-9]{40}")

const base = "gcr.io/buildpacks/gcp/run:v1"

func main() {
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
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/buildpack", http.StatusSeeOther))

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
	ctx := r.Context()

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
	if len(parts) < 4 {
		serve.Error(w, errors.New("Must specify GitHub repository"))
		return
	}
	ghOwner, ghRepo := parts[2], parts[3]
	path := strings.Join(parts[4:len(parts)-2], "/")
	tagOrDigest := parts[len(parts)-1]

	// If request is for image by digest, try to serve it from GCS.
	if strings.HasPrefix(tagOrDigest, "sha256:") {
		desc, err := s.storage.BlobExists(ctx, tagOrDigest)
		if err != nil {
			s.error.Printf("ERROR (storage.BlobExists): %s", err)
			serve.Error(w, serve.ErrNotFound)
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Docker-Content-Digest", tagOrDigest)
			w.Header().Set("Content-Type", string(desc.MediaType))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", desc.Size))
			return
		}
		serve.Blob(w, r, tagOrDigest)
		return
	}

	// If the image tag looks like a commit SHA, see if we already have a
	// manifest cached for that revision and serve it directly.  Otherwise,
	// resolve the branch/tag/whatever to a SHA and redirect to that SHA
	// image tag.
	revision := tagOrDigest
	if commitRE.MatchString(revision) {
		ck := cacheKey(path, revision)
		if _, err := s.storage.BlobExists(ctx, ck); err == nil {
			s.info.Println("serving cached manifest:", ck)
			serve.Blob(w, r, ck)
			return
		}
	} else {
		revision, err = s.resolveCommit(ghOwner, ghRepo, revision)
		if err != nil {
			s.error.Println("ERROR(resolveCommit):", err)
			serve.Error(w, err)
			return
		}
		path := r.URL.Path[:strings.LastIndex(r.URL.Path, "/")+1] + revision
		http.Redirect(w, r, path, http.StatusSeeOther)
		return
	}

	// Fetch, detect and build source.
	image, err := s.fetchAndBuild(src, layers, ghOwner, ghRepo, revision, path)
	if err != nil {
		s.error.Println("ERROR:", err)
		serve.Error(w, err)
		return
	}

	// Serve new image manifest.
	img, err := s.getImage(ctx, image)
	if err != nil {
		s.error.Println("ERROR:", err)
		serve.Error(w, err)
		return
	}

	// Serve the manifest.
	ck := cacheKey(path, revision)
	if err := s.storage.ServeManifest(w, r, img, ck); err != nil {
		s.error.Printf("ERROR (storage.ServeManifest): %v", err)
		serve.Error(w, err)
		return
	}
}

func cacheKey(path, revision string) string {
	ck := []byte(fmt.Sprintf("%s-%s", strings.ReplaceAll(path, "/", "_"), revision))
	return fmt.Sprintf("buildpack-%x", md5.Sum(ck))
}

// Resolves a ref (branch, tag, PR, commit) into its SHA.
// If the image tag is "latest", use the repo's default branch.
// If the image tag is "latest-release", look up the repo's latest release tag.
// In any case, resolve the branch/tag/whatever to a commit SHA.
func (s *server) resolveCommit(owner, repo, ref string) (string, error) {
	ctx := context.Background()
	client := github.NewClient(nil)

	if ref == "latest" {
		repo, _, err := client.Repositories.Get(ctx, owner, repo)
		if err != nil {
			return "", err
		}
		ref = repo.GetDefaultBranch()
	}
	if ref == "latest-release" {
		release, resp, err := client.Repositories.GetLatestRelease(ctx, owner, repo)
		if resp.StatusCode == http.StatusNotFound {
			return "", errors.New("Repository has no releases")
		}
		if err != nil {
			return "", err
		}
		ref = release.GetTagName()
	}

	commit, _, err := client.Repositories.GetCommit(ctx, owner, repo, ref)
	if err != nil {
		return "", err
	}
	s.info.Printf("Resolved %q -> %q\n", ref, commit.GetSHA())
	return commit.GetSHA(), nil
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

func (s *server) fetchAndBuild(src, layers, ghOwner, ghRepo, revision, path string) (string, error) {
	image := fmt.Sprintf("gcr.io/%s/built-%d", projectID, time.Now().Unix())
	source := fmt.Sprintf("https://github.com/%s/%s/archive/%s.tar.gz", ghOwner, ghRepo, revision)

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
EOF`, tok.AccessToken),
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

func (s *server) getImage(ctx context.Context, image string) (v1.Image, error) {
	ref, err := name.NewTag(image, name.WeakValidation)
	if err != nil {
		return nil, err
	}
	authn, err := gauthn.NewEnvAuthenticator()
	if err != nil {
		return nil, err
	}
	return remote.Image(ref, remote.WithAuth(authn), remote.WithContext(ctx))
}
