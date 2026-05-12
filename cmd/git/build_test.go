package main

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/google/go-containerregistry/pkg/v1/validate"
)

const (
	testRepoURL  = "https://github.com/octocat/Hello-World"
	testRepoName = "Hello-World"
	testRef      = "master"
)

// layerFiles builds the git layer for the test repo and returns a map of
// "<perm>:<sha256>" (or "symlink:<target>") keyed by tar entry name.
func layerFiles(t *testing.T, prefix string) map[string]string {
	t.Helper()
	ctx := context.Background()
	hash, refName, err := resolveRef(ctx, testRepoURL, testRef)
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	layer, err := gitLayer(ctx, testRepoURL, refName, hash, prefix)
	if err != nil {
		t.Fatalf("gitLayer: %v", err)
	}
	if err := validate.Layer(layer); err != nil {
		t.Fatalf("validate.Layer: %v", err)
	}
	rc, err := layer.Uncompressed()
	if err != nil {
		t.Fatalf("Uncompressed: %v", err)
	}
	defer rc.Close()
	files := map[string]string{}
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		switch hdr.Typeflag {
		case tar.TypeSymlink:
			files[hdr.Name] = "symlink:" + hdr.Linkname
		case tar.TypeReg:
			h := sha256.New()
			if _, err := io.Copy(h, tr); err != nil {
				t.Fatalf("read %s: %v", hdr.Name, err)
			}
			files[hdr.Name] = fmt.Sprintf("%o:%x", hdr.Mode&0777, h.Sum(nil))
		}
	}
	return files
}

// clonedWorktreeFiles does a real shallow clone and returns the same kind of
// map for the worktree files (everything outside .git).
func clonedWorktreeFiles(t *testing.T, prefix string) map[string]string {
	t.Helper()
	checkout := filepath.Join(t.TempDir(), testRepoName)
	if _, err := git.PlainCloneContext(context.Background(), checkout, false, &git.CloneOptions{
		URL:           testRepoURL,
		ReferenceName: "refs/heads/" + testRef,
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	}); err != nil {
		t.Fatalf("clone: %v", err)
	}
	files := map[string]string{}
	if err := filepath.Walk(checkout, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(checkout, p)
		if err != nil {
			return err
		}
		if rel == "." || rel == ".git" || strings.HasPrefix(rel, ".git"+string(os.PathSeparator)) {
			return nil
		}
		name := path.Join(prefix, filepath.ToSlash(rel))
		switch {
		case fi.IsDir():
		case fi.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			files[name] = "symlink:" + target
		case fi.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			files[name] = fmt.Sprintf("%o:%x", fi.Mode().Perm(), sha256.Sum256(b))
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	return files
}

func TestGitLayer(t *testing.T) {
	if testing.Short() {
		t.Skip("clones from github")
	}
	prefix := path.Join("git", testRepoName)
	got := layerFiles(t, prefix)

	// The .git directory is shipped so the git CLI works inside the checkout.
	for _, want := range []string{"HEAD", "config", "index"} {
		if _, ok := got[path.Join(prefix, ".git", want)]; !ok {
			t.Errorf("layer missing %s", path.Join(prefix, ".git", want))
		}
	}
	hasPack := false
	objectsDir := path.Join(prefix, ".git", "objects")
	for name := range got {
		if strings.HasPrefix(name, path.Join(objectsDir, "pack")+"/") && strings.HasSuffix(name, ".pack") {
			hasPack = true
		}
		// The objects must stay packed: nothing should be unpacked into loose
		// objects/<2-hex>/<38-hex>. If that ever happens the whole repo's
		// decompressed contents would be sitting in memory.
		if rest, ok := strings.CutPrefix(name, objectsDir+"/"); ok {
			if dir, _, found := strings.Cut(rest, "/"); found && dir != "pack" && dir != "info" {
				t.Errorf("loose object in layer: %s", name)
			}
		}
	}
	if !hasPack {
		t.Errorf("layer has no packfile under %s/pack/", objectsDir)
	}

	// Worktree files must match a real checkout exactly.
	gotWT := map[string]string{}
	for name, v := range got {
		if !strings.HasPrefix(name, path.Join(prefix, ".git")+"/") {
			gotWT[name] = v
		}
	}
	want := clonedWorktreeFiles(t, prefix)
	if len(gotWT) == 0 || len(want) == 0 {
		t.Fatalf("no worktree files: layer=%d clone=%d", len(gotWT), len(want))
	}
	for name, w := range want {
		if g, ok := gotWT[name]; !ok {
			t.Errorf("missing %s in layer", name)
		} else if g != w {
			t.Errorf("%s: layer=%s clone=%s", name, g, w)
		}
	}
	for name := range gotWT {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected %s in layer", name)
		}
	}
}
