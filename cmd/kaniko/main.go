package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
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

const base = "packs/run:v3alpha2"

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
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/master/cmd/kaniko", http.StatusSeeOther))

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
		s.serveKanikoManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

func (s *server) serveKanikoManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Prepare workspace.
	if err := s.prepareWorkspace(); err != nil {
		s.error.Println("ERROR:", err)
		serve.Error(w, err)
		return
	}
	// Clean up workspace.
	defer func() {
		if err := os.RemoveAll("/tmp"); err != nil {
			s.error.Printf("RemoveAll(/tmp): %v", err)
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
	revision := parts[len(parts)-1]

	// If the image tag looks like a commit SHA, see if we already have a
	// manifest cached for that revision and serve it directly.  Otherwise,
	// resolve the branch/tag/whatever to a SHA and redirect to that SHA
	// image tag.
	if commitRE.MatchString(revision) {
		ck := cacheKey(path, revision)
		if err := s.storage.BlobExists(ctx, ck); err == nil {
			s.info.Println("serving cached manifest:", ck)
			serve.Blob(w, r, ck)
			return
		}
	} else {
		revision, err := s.resolveCommit(ghOwner, ghRepo, revision)
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
	image, err := s.fetchAndBuild(ghOwner, ghRepo, revision, path)
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

	// Serve the manifest.
	ck := cacheKey(path, revision)
	if err := s.storage.ServeManifest(w, r, img, ck); err != nil {
		s.error.Printf("ERROR (storage.ServeManifest): %v", err)
		serve.Error(w, err)
		return
	}
}

func cacheKey(path, revision string) string {
	return fmt.Sprintf("buildpack-%s-%s", strings.ReplaceAll(path, "/", "_"), revision)
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

func (s *server) prepareWorkspace() error {
	// Create tempdir to store app source.
	src, err := ioutil.TempDir("", "")
	if err != nil {
		return err
	}

	// cd into the temp dir.
	if err := os.Chdir(src); err != nil {
		return err
	}

	// Create and set $HOME, which is otherwise not writable.
	home, err := ioutil.TempDir("", "")
	if err != nil {
		return err
	}
	os.Setenv("HOME", home)

	return nil
}

func (s *server) fetchAndBuild(ghOwner, ghRepo, revision, path string) (string, error) {
	image := fmt.Sprintf("gcr.io/%s/built-%d", projectID, time.Now().Unix)
	source := fmt.Sprintf("https://github.com/%s/%s/archive/%s.tar.gz", ghOwner, ghRepo, revision)

	if resp, err := http.Head(source); err != nil {
		return "", err
	} else if resp.StatusCode == http.StatusNotFound {
		return "", serve.ErrNotFound
	} else if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HEAD %s (%d): %s", source, resp.StatusCode, resp.Status)
	}

	ts, err := google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("google.DefaultTokenSource: %v", err)
	}
	tok, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("credentials.Token: %v", err)
	}

	for _, cmd := range []string{
		fmt.Sprintf("wget -qO- %s | tar xz --strip-components=1 -C .", source),
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
		fmt.Sprintf(`
/kaniko/executor --force \
  --dockerfile=./%s/Dockerfile \
  --context=./%s \
  --destination=%s \
  --cache-repo=gcr.io/%s`, path, path, image, projectID),
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
	authn, err := gauthn.NewEnvAuthenticator()
	if err != nil {
		return nil, err
	}
	return remote.Image(ref, remote.WithAuth(authn))
}
