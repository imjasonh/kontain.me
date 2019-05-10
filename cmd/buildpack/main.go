package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/logging"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/imjasonh/kontain.me/pkg/run"
	"github.com/imjasonh/kontain.me/pkg/serve"
	"golang.org/x/oauth2/google"
)

var projectID = ""

func init() {
	p, err := metadata.ProjectID()
	if err != nil {
		log.Fatalf("metadata.ProjectID: %v", err)
	}
	projectID = p
}

const base = "packs/run:v3alpha2"

func main() {
	ctx := context.Background()
	client, err := logging.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("logging.NewClient: %v", err)
	}
	lg := client.Logger("server")

	http.Handle("/v2/", &server{
		info:  lg.StandardLogger(logging.Info),
		error: lg.StandardLogger(logging.Error),
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
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.info.Println("handler:", r.Method, r.URL)
	path := strings.TrimPrefix(r.URL.String(), "/v2/")

	switch {
	case path == "": // API Version check.
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	case strings.Contains(path, "/manifests/"):
		s.serveBuildpackManifest(w, r)
	case strings.Contains(path, "/blobs/"):
		serve.Blob(w, r)
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
	path := strings.TrimPrefix(r.URL.Path, "/v2/ko/")
	parts := strings.Split(path, "/")
	repo := strings.Join(parts[2:len(parts)-2], "/")
	if repo == "" || repo == "buildpack" {
		repo = "buildpack/sample-java-app"
	}
	revision := parts[len(parts)-1]
	if revision == "latest" {
		revision = "master"
	}

	// Fetch, detect and build source.
	image, err := s.fetchAndBuild(src, layers, repo, revision)
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
	serve.Manifest(w, img)
}

func (s *server) prepareWorkspace() (string, string, error) {
	// Write Docker config.
	tok, err := google.ComputeTokenSource("").Token()
	if err != nil {
		return "", "", fmt.Errorf("getting access token from metadata: %v", err)
	}
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("oauth2accesstoken:%s", tok.AccessToken)))
	configJSON := fmt.Sprintf(`{
	"auths": {
		"https://gcr.io": {
			"auth": %q
		}
	}
}`, auth)
	if err := run.Do(s.info.Writer(), "mkdir -p ~/.docker/ && cat << EOF > ~/.docker/config.json\n"+string(configJSON)+"\nEOF"); err != nil {
		return "", "", err
	}

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

	// Create and set $HOME.
	home, err := ioutil.TempDir("", "")
	if err != nil {
		return "", "", err
	}
	os.Setenv("HOME", home)

	return src, layers, nil
}

func (s *server) fetchAndBuild(src, layers, repo, revision string) (string, error) {
	image := fmt.Sprintf("gcr.io/%s/built-%d", projectID, time.Now().Unix)
	source := fmt.Sprintf("https://github.com/%s/archive/%s.tar.gz", repo, revision)

	if resp, err := http.Head(source); err != nil {
		return "", err
	} else if resp.StatusCode == http.StatusNotFound {
		return "", serve.ErrNotFound
	} else if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HEAD %s (%d): %s", source, resp.StatusCode, resp.Status)
	}

	for _, cmd := range []string{
		fmt.Sprintf("chown -R %d:%d %s", os.Geteuid(), os.Getgid(), src),
		fmt.Sprintf("chown -R %d:%d %s", os.Geteuid(), os.Getgid(), layers),
		fmt.Sprintf("wget -qO- %s | tar xvz --strip-components=1 -C %s", source, src),
		fmt.Sprintf("ls -R %s", src),
		fmt.Sprintf("/lifecycle/detector -app=%s -group=%s/group.toml -plan=%s/plan.toml", src, layers, layers),
		fmt.Sprintf("/lifecycle/analyzer -layers=%s -helpers=true -group=%s/group.toml %s", layers, layers, image),
		fmt.Sprintf("/lifecycle/builder -layers=%s -app=%s -group=%s/group.toml -plan=%s/plan.toml", layers, src, layers, layers),
		fmt.Sprintf("/lifecycle/exporter -layers=%s -helpers=true -app=%s -image=%s -group=%s/group.toml %s", layers, src, base, layers, image),
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
