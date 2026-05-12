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
	"strconv"
	"strings"
	"time"

	"github.com/chainguard-dev/clog/gcp"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	billyutil "github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
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

	img, err := build(ctx, cloneURL, repoName, refName, hash)
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

// build produces a single-layer image containing a checkout of the requested
// commit at /git/<repoName>, alongside the (shallow) .git directory, on top of
// the git base image.
func build(ctx context.Context, cloneURL, repoName string, refName plumbing.ReferenceName, hash plumbing.Hash) (v1.Image, error) {
	base, err := remote.Image(baseRef)
	if err != nil {
		return nil, fmt.Errorf("remote.Image(%q): %w", baseRef, err)
	}
	bcf, err := base.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("base ConfigFile: %w", err)
	}
	// Own the checkout as the user the image runs as, so `git` can write to it
	// (e.g. `git fetch --unshallow`) without a permission error.
	uid, gid := imageUser(bcf.Config.User)

	layer, err := gitLayer(ctx, cloneURL, refName, hash, path.Join("git", repoName), uid, gid)
	if err != nil {
		return nil, err
	}
	cfgLayer, err := gitConfigLayer()
	if err != nil {
		return nil, fmt.Errorf("gitConfigLayer: %w", err)
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

// gitLayer clones cloneURL into an in-memory git directory and returns a layer
// containing a checkout of the requested commit (every entry rooted under
// prefix, e.g. "git/<repoName>") alongside the (shallow) .git directory.
//
// Only the fetched packfile is buffered in memory (so offset deltas can be
// resolved); worktree file contents are streamed straight into the layer tar
// as the commit's tree is walked, rather than being unpacked to disk first.
func gitLayer(ctx context.Context, cloneURL string, refName plumbing.ReferenceName, hash plumbing.Hash, prefix string, uid, gid int) (v1.Layer, error) {
	dotgit := memfs.New()
	store := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

	var co git.CloneOptions
	if refName == "" {
		// A bare commit SHA: full clone, then resolve that commit.
		co = git.CloneOptions{URL: cloneURL, Tags: git.NoTags, NoCheckout: true}
	} else {
		// A branch or tag: shallow, single-branch clone.
		refn := refName
		if refn == plumbing.HEAD {
			refn = "" // let go-git pick the default branch
		}
		co = git.CloneOptions{
			URL:           cloneURL,
			ReferenceName: refn,
			SingleBranch:  true,
			Depth:         1,
			Tags:          git.NoTags,
			NoCheckout:    true,
		}
	}
	repo, err := git.CloneContext(ctx, store, nil, &co)
	if err != nil {
		return nil, fmt.Errorf("clone %q: %w", cloneURL, err)
	}

	commitHash := hash
	if refName != "" {
		head, err := repo.Head()
		if err != nil {
			return nil, fmt.Errorf("resolving HEAD of %q: %w", cloneURL, err)
		}
		commitHash = head.Hash()
	}
	commit, err := resolveCommit(repo, commitHash)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("tree for %s: %w", commit.Hash, err)
	}

	// The image ships a worktree, so the .git directory we copy out isn't bare;
	// also record a .git/index for the checkout so `git status` looks clean.
	if cfg, err := repo.Config(); err == nil {
		cfg.Core.IsBare = false
		_ = repo.Storer.SetConfig(cfg)
	}
	if err := writeIndex(repo, tree); err != nil {
		return nil, fmt.Errorf("writing index: %w", err)
	}

	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go func() { pw.CloseWithError(writeRepoTar(pw, dotgit, tree, prefix, uid, gid)) }()
		return pr, nil
	})
	if err != nil {
		return nil, fmt.Errorf("LayerFromOpener: %w", err)
	}
	return layer, nil
}

var epoch = time.Unix(0, 0)

// tarDir writes a directory header named name (with a trailing slash), owned by
// uid:gid, mode 0755.
func tarDir(tw *tar.Writer, name string, uid, gid int) error {
	return tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     strings.TrimSuffix(name, "/") + "/",
		Mode:     0755,
		Uid:      uid,
		Gid:      gid,
		ModTime:  epoch,
	})
}

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

// imageUser returns the numeric uid/gid an image's configured User runs as
// (e.g. "65532" or "65532:65532"). A non-numeric or empty User maps to root.
func imageUser(user string) (uid, gid int) {
	u, g, hasGroup := strings.Cut(user, ":")
	uid, _ = strconv.Atoi(u)
	if !hasGroup {
		return uid, uid
	}
	gid, _ = strconv.Atoi(g)
	return uid, gid
}

// resolveCommit returns the commit identified by h, dereferencing annotated tag
// objects as needed.
func resolveCommit(repo *git.Repository, h plumbing.Hash) (*object.Commit, error) {
	for {
		if c, err := repo.CommitObject(h); err == nil {
			return c, nil
		}
		t, err := repo.TagObject(h)
		if err != nil {
			return nil, fmt.Errorf("resolving %s to a commit: %w", h, err)
		}
		h = t.Target
	}
}

// writeIndex records a .git/index describing the contents of tree, so the
// checked-out worktree looks clean to git even though it's never materialized
// on disk during the build.
func writeIndex(repo *git.Repository, tree *object.Tree) error {
	idx := &index.Index{Version: 2}
	if err := tree.Files().ForEach(func(f *object.File) error {
		idx.Entries = append(idx.Entries, &index.Entry{
			Name: f.Name,
			Hash: f.Hash,
			Mode: f.Mode,
			Size: uint32(f.Size),
		})
		return nil
	}); err != nil {
		return err
	}
	return repo.Storer.SetIndex(idx)
}

// writeRepoTar streams a tar of the checked-out worktree (every entry rooted
// under prefix, e.g. "git/<repoName>") followed by the repository's .git
// directory. Worktree file contents are read straight from the in-memory
// packfile via tree; only .git itself lives in dotgit.
func writeRepoTar(w io.Writer, dotgit billy.Filesystem, tree *object.Tree, prefix string, uid, gid int) error {
	tw := tar.NewWriter(w)

	// Emit prefix's ancestor directories ("git/", "git/<repoName>/") and, below,
	// the ".git" directory itself, owned by uid:gid — otherwise the image
	// extractor auto-creates them root-owned and git can't write inside them
	// (e.g. `git fetch` creating .git/FETCH_HEAD).
	dirs := strings.Split(prefix, "/")
	for i := range dirs {
		if err := tarDir(tw, strings.Join(dirs[:i+1], "/"), uid, gid); err != nil {
			tw.Close()
			return err
		}
	}

	walker := object.NewTreeWalker(tree, true, nil)
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			walker.Close()
			tw.Close()
			return err
		}
		if err := writeWorktreeEntry(tw, tree, path.Join(prefix, name), entry, uid, gid); err != nil {
			walker.Close()
			tw.Close()
			return err
		}
	}
	walker.Close()

	gitDir := path.Join(prefix, ".git")
	if err := tarDir(tw, gitDir, uid, gid); err != nil {
		tw.Close()
		return err
	}
	if err := billyutil.Walk(dotgit, "/", func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel := strings.Trim(filepath.ToSlash(p), "/")
		if rel == "" {
			return nil
		}
		name := path.Join(gitDir, rel)
		if fi.IsDir() {
			return tarDir(tw, name, uid, gid)
		}
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     name,
			Mode:     0644,
			Uid:      uid,
			Gid:      gid,
			Size:     fi.Size(),
			ModTime:  epoch,
		}); err != nil {
			return err
		}
		f, err := dotgit.Open(p)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}); err != nil {
		tw.Close()
		return err
	}
	return tw.Close()
}

func writeWorktreeEntry(tw *tar.Writer, tree *object.Tree, name string, entry object.TreeEntry, uid, gid int) error {
	switch entry.Mode {
	case filemode.Dir:
		return tarDir(tw, name, uid, gid)
	case filemode.Submodule:
		// Submodules have no file content of their own.
		return nil
	}

	f, err := tree.TreeEntryFile(&entry)
	if err != nil {
		return err
	}

	if entry.Mode == filemode.Symlink {
		target, err := f.Contents()
		if err != nil {
			return err
		}
		return tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeSymlink,
			Name:     name,
			Linkname: target,
			Mode:     0777,
			Uid:      uid,
			Gid:      gid,
			ModTime:  epoch,
		})
	}

	mode := int64(0644)
	if entry.Mode == filemode.Executable {
		mode = 0755
	}
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     mode,
		Uid:      uid,
		Gid:      gid,
		Size:     f.Size,
		ModTime:  epoch,
	}); err != nil {
		return err
	}
	rc, err := f.Reader()
	if err != nil {
		return err
	}
	_, err = io.Copy(tw, rc)
	if cerr := rc.Close(); err == nil {
		err = cerr
	}
	return err
}
