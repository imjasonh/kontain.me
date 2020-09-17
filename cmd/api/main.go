package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/datastore"
	"cloud.google.com/go/storage"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/uuid"
	"github.com/imjasonh/kontain.me/pkg/run"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcb "google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/option"
)

const base = "packs/run:v3alpha2"

var (
	projectRE = regexp.MustCompile("/v1/projects/([a-z0-9-]+)/")
	buildRE   = regexp.MustCompile("/v1/projects/[a-z0-9-]+/builds/([a-z0-9-]+)")
)

func main() {
	ctx := context.Background()
	projectID, err := metadata.ProjectID()
	if err != nil {
		log.Fatalf("metadata.ProjectID: %v", err)
	}
	ds, err := datastore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("datastore.NewClient: %v", err)
	}

	http.Handle("/", &server{
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

func extractPath(path string, re *regexp.Regexp) string {
	found := re.FindStringSubmatch(path)
	if len(found) < 2 {
		return ""
	}
	return found[1]
}

func extractToken(r *http.Request) string {
	hdr := r.Header.Get("Authorization")
	if strings.HasPrefix(hdr, "Bearer ") {
		return strings.TrimPrefix(hdr, "Bearer ")
	}
	return r.URL.Query().Get("access_token")
}

func (s *server) logWriter(req *gcb.Build, tok string) io.WriteCloser {
	ctx := context.Background() // TODO
	gcs, err := storage.NewClient(ctx, option.WithTokenSource(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: tok})))
	if err != nil {
		log.Fatalf("storage.NewClient: %v", err)
	}
	return gcs.Bucket(req.LogsBucket).Object(fmt.Sprintf("log-%s.txt", req.Id)).NewWriter(ctx)
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.info.Println("handler:", r.Method, r.URL)
	projectID := extractPath(r.URL.Path, projectRE)
	if projectID == "" {
		http.Error(w, "missing project", http.StatusBadRequest)
		return
	}
	buildID := extractPath(r.URL.Path, buildRE)

	switch {
	case r.Method == http.MethodGet && buildID != "":
		s.getBuild(w, r, buildID)
	case r.Method == http.MethodPost && buildID == "":
		s.createBuild(w, r, projectID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *server) getBuild(w http.ResponseWriter, r *http.Request, buildID string) {
	io.Copy(w, bytes.NewReader(s.get(buildID)))
}

func (s *server) createBuild(w http.ResponseWriter, r *http.Request, projectID string) {
	start := time.Now()
	tok := extractToken(r)
	if tok == "" {
		http.Error(w, "bad auth", http.StatusUnauthorized)
		return
	}

	defer r.Body.Close()
	var req gcb.Build
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error.Printf("json.Decode: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Id = uuid.New().String()
	req.ProjectId = projectID
	req.CreateTime = start.Format(time.RFC3339Nano)
	req.StartTime = req.CreateTime
	req.LogsBucket = req.Source.StorageSource.Bucket // TODO: actually write logs somewhere.

	// Do the build...
	if err := s.buildImage(&req, tok); err != nil {
		req.Status = "FAILURE"
		req.StatusDetail = err.Error()
	} else {
		req.Status = "SUCCESS"
	}
	req.FinishTime = time.Now().Format(time.RFC3339Nano)

	bomd, err := json.Marshal(&gcb.BuildOperationMetadata{Build: &req})
	if err != nil {
		s.error.Printf("json.Encode: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(&gcb.Operation{
		Name:     base64.StdEncoding.EncodeToString([]byte(req.Id)),
		Done:     true,
		Metadata: bomd,
	}); err != nil {
		s.error.Printf("Encode: %v", err)
	}
	s.put(req)
}

type e struct {
	Bytes []byte `datastore:",noindex"`
}

func (s *server) put(req gcb.Build) {
	ctx := context.Background() // TODO
	k := datastore.NameKey("Builds", req.Id, nil)
	b, err := json.Marshal(req)
	if err != nil {
		s.error.Printf("json.Marshal: %v", err)
		return
	}
	if _, err := s.ds.Put(ctx, k, &e{b}); err != nil {
		s.error.Printf("datastore.Put: %v", err)
	}
}

func (s *server) get(id string) []byte {
	ctx := context.Background() // TODO
	k := datastore.NameKey("Builds", id, nil)
	var e e
	if err := s.ds.Get(ctx, k, &e); err != nil {
		s.error.Printf("datastore.Get: %v", err)
	}
	return e.Bytes
}

func (s *server) buildImage(req *gcb.Build, tok string) error {
	// Validate request.
	if err := s.validate(req); err != nil {
		return err
	}

	// Prepare workspace.
	src, err := s.prepareWorkspace(tok)
	if err != nil {
		return err
	}
	// Clean up workspace.
	defer func() {
		for _, path := range []string{
			src, os.Getenv("HOME"),
		} {
			if err := os.RemoveAll(path); err != nil {
				s.error.Printf("RemoveAll(%q): %v", path, err)
			}
		}
		os.Setenv("HOME", "/home/")
	}()

	// Fetch, detect and build image.
	if err := s.fetchAndBuild(src, tok, req); err != nil {
		return err
	}

	// Get the digest of the image we just pushed.
	if img, err := s.getImage(req.Images[0]); err != nil {
		return err
	} else {
		d, err := img.Digest()
		if err != nil {
			return err
		}
		req.Results = &gcb.Results{
			Images: []*gcb.BuiltImage{{
				Name:   req.Images[0],
				Digest: d.String(),
			}},
		}
	}
	return nil
}

func (s *server) validate(req *gcb.Build) error {
	if len(req.Images) != 1 {
		return errors.New("must request exactly one image")
	}
	if req.Source.StorageSource.Bucket == "" ||
		req.Source.StorageSource.Object == "" {
		return errors.New("must request bucket and object")
	}
	return nil
}

func (s *server) prepareWorkspace(tok string) (string, error) {
	// Create and set $HOME.
	home, err := ioutil.TempDir("", "")
	if err != nil {
		return "", err
	}
	os.Setenv("HOME", home)

	// Write Docker config with user credentials.
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("oauth2accesstoken:%s", tok)))
	configJSON := fmt.Sprintf(`{
	"auths": {
		"https://gcr.io": {
			"auth": %q
		}
	}
}`, auth)
	if err := run.Do(s.info.Writer(), "mkdir -p $HOME/.docker/ && cat << EOF > $HOME/.docker/config.json\n"+string(configJSON)+"\nEOF"); err != nil {
		return "", err
	}

	// Create tempdir to store app source.
	return ioutil.TempDir("", "")
}

func (s *server) fetchAndBuild(src, tok string, req *gcb.Build) error {
	image := req.Images[0]
	source := fmt.Sprintf("https://storage.googleapis.com/%s/%s?access_token=%s", req.Source.StorageSource.Bucket, req.Source.StorageSource.Object, tok)
	w := s.logWriter(req, tok)
	defer func() {
		if err := w.Close(); err != nil {
			s.error.Printf("Closing GCS log: %v", err)
		}
	}()

	ts, err := google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("google.DefaultTokenSource: %v", err)
	}
	tok, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("credentials.Token: %v", err)
	}

	for _, cmd := range []string{
		fmt.Sprintf("mkdir -p /tmp/layers"),
		fmt.Sprintf("chown -R %d:%d %s", os.Geteuid(), os.Getgid(), src),
		fmt.Sprintf("chown -R %d:%d /tmp/layers", os.Geteuid(), os.Getgid()),
		fmt.Sprintf("curl -fsSL %s | tar xz -C %s", source, src),
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
		fmt.Sprintf("/lifecycle/detector -app=%s -group=/tmp/layers/group.toml -plan=/tmp/layers/plan.toml", src),
		fmt.Sprintf("/lifecycle/analyzer -layers=/tmp/layers -helpers=false -group=/tmp/layers/group.toml %s", image),
		fmt.Sprintf("/lifecycle/builder -layers=/tmp/layers -app=%s -group=/tmp/layers/group.toml -plan=/tmp/layers/plan.toml", src),
		fmt.Sprintf("/lifecycle/exporter -layers=/tmp/layers -helpers=false -app=%s -image=%s -group=/tmp/layers/group.toml %s", src, base, image),
	} {
		if err := run.Do(io.MultiWriter(s.info.Writer(), w), cmd); err != nil {
			return fmt.Errorf("Running %q: %v", cmd, err)
		}
	}
	return nil
}

func (s *server) getImage(image string) (v1.Image, error) {
	ref, err := name.NewTag(image)
	if err != nil {
		return nil, err
	}
	return remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
}
