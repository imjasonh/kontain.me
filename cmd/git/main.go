package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chainguard-dev/clog/gcp"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/imjasonh/kontain.me/pkg/serve"
)

func main() {
	ctx := context.Background()
	st, err := serve.NewStorage(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "serve.NewStorage", "err", err)
		os.Exit(1)
	}
	http.Handle("/v2/", gcp.WithCloudTraceContext(&server{storage: st}))
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/git", http.StatusSeeOther))

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
		s.serveGitManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

func cacheKey(cloneURL string, hash plumbing.Hash) string {
	return fmt.Sprintf("git-%x", md5.Sum([]byte(cloneURL+"@"+hash.String())))
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// git.kontain.me/github.com/imjasonh/kontain.me:main
//
//	-> shallow-clone https://github.com/imjasonh/kontain.me at "main",
//	   serve an image with it checked out at /git/kontain.me
//
// The image tag is the ref: a branch, a tag, or a full commit SHA. The default
// tag "latest" resolves to the remote's default branch unless a branch or tag
// named "latest" exists. A trailing ".git" on the repo path is tolerated.
func (s *server) serveGitManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	upath := strings.TrimPrefix(r.URL.Path, "/v2/")
	parts := strings.Split(upath, "/")
	if len(parts) < 3 {
		serve.Error(w, serve.ErrNotFound)
		return
	}
	// Path is "<host>/<path>/manifests/<ref>"; the ref is the image tag.
	ref := parts[len(parts)-1]
	repoPath := strings.Join(parts[:len(parts)-2], "/")
	for strings.HasPrefix(repoPath, "git.kontain.me/") {
		repoPath = strings.TrimPrefix(repoPath, "git.kontain.me/")
	}
	if !strings.Contains(repoPath, "/") {
		serve.Error(w, fmt.Errorf("invalid repo path %q: want <host>/<path>", repoPath))
		return
	}
	cloneURL := "https://" + repoPath
	repoName := strings.TrimSuffix(path.Base(repoPath), ".git")

	// Resolve the requested ref to a commit so we can check the cache before
	// cloning.
	hash, refName, err := resolveRef(ctx, cloneURL, ref)
	if err != nil {
		slog.ErrorContext(ctx, "resolveRef", "url", cloneURL, "ref", ref, "err", err)
		serve.Error(w, err)
		return
	}

	ck := cacheKey(cloneURL, hash)
	if _, err := s.storage.BlobExists(ctx, ck); err == nil {
		slog.InfoContext(ctx, "serving cached manifest", "ck", ck)
		serve.Blob(w, r, ck)
		return
	}

	dir, err := os.MkdirTemp("", "git-*")
	if err != nil {
		slog.ErrorContext(ctx, "MkdirTemp", "err", err)
		serve.Error(w, err)
		return
	}
	defer os.RemoveAll(dir)

	img, err := build(ctx, dir, cloneURL, repoName, refName, hash)
	if err != nil {
		slog.ErrorContext(ctx, "build", "url", cloneURL, "ref", ref, "err", err)
		serve.Error(w, err)
		return
	}

	if err := s.storage.ServeManifest(w, r, img, ck); err != nil {
		slog.ErrorContext(ctx, "storage.ServeManifest", "err", err)
		serve.Error(w, err)
	}
}

// resolveRef resolves ref (an OCI image tag) against the remote and returns the
// commit it points to plus the reference name to clone. ref is matched first as
// a branch, then as a tag; the default tag "latest" otherwise resolves to the
// remote's default branch. A full commit SHA matching no branch or tag is used
// as-is, with an empty refName so the caller checks it out from a full clone.
func resolveRef(ctx context.Context, cloneURL, ref string) (plumbing.Hash, plumbing.ReferenceName, error) {
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{cloneURL},
	})
	refs, err := rem.ListContext(ctx, &git.ListOptions{})
	if err != nil {
		return plumbing.ZeroHash, "", err
	}
	byName := make(map[plumbing.ReferenceName]*plumbing.Reference, len(refs))
	var head *plumbing.Reference
	for _, r := range refs {
		byName[r.Name()] = r
		if r.Name() == plumbing.HEAD {
			head = r
		}
	}

	if r, ok := byName[plumbing.NewBranchReferenceName(ref)]; ok {
		return r.Hash(), r.Name(), nil
	}
	if r, ok := byName[plumbing.NewTagReferenceName(ref)]; ok {
		return r.Hash(), r.Name(), nil
	}
	if ref == "latest" || ref == "" {
		// No branch or tag named "latest": use the remote's default branch.
		if head == nil {
			return plumbing.ZeroHash, "", fmt.Errorf("remote %q advertises no HEAD", cloneURL)
		}
		if head.Type() == plumbing.SymbolicReference {
			t, ok := byName[head.Target()]
			if !ok {
				return plumbing.ZeroHash, "", fmt.Errorf("remote %q HEAD points to unknown ref %q", cloneURL, head.Target())
			}
			return t.Hash(), t.Name(), nil
		}
		return head.Hash(), plumbing.HEAD, nil
	}
	if fullSHA.MatchString(ref) {
		return plumbing.NewHash(ref), "", nil
	}
	return plumbing.ZeroHash, "", fmt.Errorf("ref %q not found in %q: not a branch, tag, or full commit SHA", ref, cloneURL)
}

// baseRef contains git and busybox
var baseRef = name.MustParseReference("cgr.dev/chainguard/git:latest-dev")

// build clones cloneURL into dir/repoName and produces a single-layer image
// containing that checkout at /git/<repoName>.
func build(ctx context.Context, dir, cloneURL, repoName string, refName plumbing.ReferenceName, hash plumbing.Hash) (v1.Image, error) {
	checkout := filepath.Join(dir, repoName)

	if refName == "" {
		// A bare commit SHA: full clone, then check the commit out.
		repo, err := git.PlainCloneContext(ctx, checkout, false, &git.CloneOptions{
			URL:  cloneURL,
			Tags: git.NoTags,
		})
		if err != nil {
			return nil, fmt.Errorf("clone %q: %w", cloneURL, err)
		}
		wt, err := repo.Worktree()
		if err != nil {
			return nil, err
		}
		if err := wt.Checkout(&git.CheckoutOptions{Hash: hash}); err != nil {
			return nil, fmt.Errorf("checkout %s: %w", hash, err)
		}
	} else {
		// A branch or tag: shallow, single-branch clone.
		refn := refName
		if refn == plumbing.HEAD {
			refn = "" // let go-git pick the default branch
		}
		if _, err := git.PlainCloneContext(ctx, checkout, false, &git.CloneOptions{
			URL:           cloneURL,
			ReferenceName: refn,
			SingleBranch:  true,
			Depth:         1,
			Tags:          git.NoTags,
		}); err != nil {
			return nil, fmt.Errorf("clone %q at %q: %w", cloneURL, refName, err)
		}
	}

	layer, err := tarball.LayerFromOpener(tarDirOpener(checkout, path.Join("git", repoName)))
	if err != nil {
		return nil, fmt.Errorf("LayerFromOpener: %w", err)
	}
	cfgLayer, err := gitConfigLayer()
	if err != nil {
		return nil, fmt.Errorf("gitConfigLayer: %w", err)
	}
	base, err := remote.Image(baseRef)
	if err != nil {
		return nil, fmt.Errorf("remote.Image(%q): %w", baseRef, err)
	}
	img, err := mutate.AppendLayers(base, layer, cfgLayer)
	if err != nil {
		return nil, fmt.Errorf("AppendLayers: %w", err)
	}
	cf, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	cf = cf.DeepCopy()
	cf.Author = "git.kontain.me"
	cf.OS = "linux"
	cf.Architecture = "amd64"
	cf.Config.WorkingDir = path.Join("/git", repoName)
	cf.Config.Entrypoint = []string{"sh"}
	return mutate.ConfigFile(img, cf)
}

var epoch = time.Unix(0, 0)

// gitConfigLayer is a layer adding /etc/gitconfig that marks every directory as
// safe, so the bundled git won't refuse to operate on the checkout when the
// image runs as a different user than wrote the files (the "dubious ownership"
// error).
func gitConfigLayer() (v1.Layer, error) {
	const cfg = "[safe]\n\tdirectory = *\n"
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "etc/gitconfig",
		Mode:     0644,
		Size:     int64(len(cfg)),
		ModTime:  epoch,
	}); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(tw, cfg); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	return tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(b)), nil
	})
}

// tarDirOpener returns an opener that produces a tar stream of dir, with every
// entry rooted under prefix.
func tarDirOpener(dir, prefix string) tarball.Opener {
	return func() (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go func() { pw.CloseWithError(writeTar(pw, dir, prefix)) }()
		return pr, nil
	}
}

func writeTar(w io.Writer, dir, prefix string) error {
	tw := tar.NewWriter(w)
	if err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		var link string
		if fi.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(fi, link)
		if err != nil {
			return err
		}
		hdr.Name = path.Join(prefix, filepath.ToSlash(rel))
		if fi.IsDir() {
			hdr.Name += "/"
		}
		hdr.ModTime, hdr.AccessTime, hdr.ChangeTime = epoch, epoch, epoch
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if fi.Mode().IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		tw.Close()
		return err
	}
	return tw.Close()
}
