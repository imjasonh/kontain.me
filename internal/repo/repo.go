package repo

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/imjasonh/kontain.me/internal/object"
)

// Repository represents a Git repository.
type Repository struct {
	path   string
	gitDir string
	mu     sync.Mutex
	count  int64
}

// New creates or opens a Git repository at the given path.
// initialFiles specifies the files to include in the initial commit.
func New(path string, initialFiles map[string][]byte) (*Repository, error) {
	repo := &Repository{
		path:   path,
		gitDir: filepath.Join(path, ".git"),
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("creating repo directory: %w", err)
	}

	// Check if it's already a git repo
	if _, err := os.Stat(repo.gitDir); os.IsNotExist(err) {
		// Initialize new repository
		if err := repo.init(); err != nil {
			return nil, fmt.Errorf("initializing repository: %w", err)
		}

		// Create initial commit
		if err := repo.createInitialCommit(initialFiles); err != nil {
			return nil, fmt.Errorf("creating initial commit: %w", err)
		}
	}

	return repo, nil
}

// init creates the Git directory structure.
func (r *Repository) init() error {
	// Create .git directory structure
	dirs := []string{
		r.gitDir,
		filepath.Join(r.gitDir, "objects"),
		filepath.Join(r.gitDir, "refs"),
		filepath.Join(r.gitDir, "refs", "heads"),
		filepath.Join(r.gitDir, "refs", "tags"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	// Create HEAD file pointing to main branch
	headPath := filepath.Join(r.gitDir, "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0644); err != nil {
		return fmt.Errorf("creating HEAD: %w", err)
	}

	// Create config file
	configPath := filepath.Join(r.gitDir, "config")
	config := `[core]
	repositoryformatversion = 0
	filemode = true
	bare = false
	logallrefupdates = true
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("creating config: %w", err)
	}

	return nil
}

// createInitialCommit creates the first commit in the repository.
func (r *Repository) createInitialCommit(files map[string][]byte) error {
	tree := object.NewTree()

	for name, content := range files {
		blob := object.NewBlob(content)
		blobHash, err := object.Write(r.gitDir, blob)
		if err != nil {
			return fmt.Errorf("writing blob for %s: %w", name, err)
		}
		tree.AddEntry("100644", name, blobHash)

		// Also write to working directory
		filePath := filepath.Join(r.path, name)
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			return fmt.Errorf("writing %s to working directory: %w", name, err)
		}
	}

	treeHash, err := object.Write(r.gitDir, tree)
	if err != nil {
		return fmt.Errorf("writing tree: %w", err)
	}

	commit := object.NewCommit(
		treeHash,
		"", // No parent for initial commit
		"Infinite Git <infinite@example.com>",
		"Infinite Git <infinite@example.com>",
		"Initial commit",
	)
	commitHash, err := object.Write(r.gitDir, commit)
	if err != nil {
		return fmt.Errorf("writing commit: %w", err)
	}

	refPath := filepath.Join(r.gitDir, "refs", "heads", "main")
	if err := os.WriteFile(refPath, []byte(commitHash+"\n"), 0644); err != nil {
		return fmt.Errorf("updating ref: %w", err)
	}

	return nil
}

// Path returns the repository path.
func (r *Repository) Path() string {
	return r.path
}

// GitDir returns the .git directory path.
func (r *Repository) GitDir() string {
	return r.gitDir
}

// Lock acquires the repository mutex. Use this to perform atomic
// read-modify-write operations spanning multiple repo calls.
func (r *Repository) Lock() { r.mu.Lock() }

// Unlock releases the repository mutex.
func (r *Repository) Unlock() { r.mu.Unlock() }

// GetRefs returns the current refs in the repository.
func (r *Repository) GetRefs() (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.getRefs()
}

// GetRefsLocked is the unlocked implementation of GetRefs.
// Caller must already hold r.mu via Lock().
func (r *Repository) GetRefsLocked() (map[string]string, error) {
	return r.getRefs()
}

// getRefs is the internal unlocked implementation of GetRefs.
// Caller must hold r.mu.
func (r *Repository) getRefs() (map[string]string, error) {
	refs := make(map[string]string)

	// Read refs from refs directory
	refsDir := filepath.Join(r.gitDir, "refs")
	err := filepath.Walk(refsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Read ref content
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Get ref name relative to .git
		relPath, err := filepath.Rel(r.gitDir, path)
		if err != nil {
			return err
		}

		refs[relPath] = strings.TrimSpace(string(content))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading refs: %w", err)
	}

	// Read HEAD
	headPath := filepath.Join(r.gitDir, "HEAD")
	headContent, err := os.ReadFile(headPath)
	if err != nil {
		return nil, fmt.Errorf("reading HEAD: %w", err)
	}

	headStr := strings.TrimSpace(string(headContent))
	if strings.HasPrefix(headStr, "ref: ") {
		// HEAD is a symbolic ref
		refName := strings.TrimPrefix(headStr, "ref: ")
		if hash, ok := refs[refName]; ok {
			refs["HEAD"] = hash
		}
	} else {
		// HEAD is a direct hash
		refs["HEAD"] = headStr
	}

	return refs, nil
}

// GetCapabilities returns the Git capabilities this server supports.
func (r *Repository) GetCapabilities() []string {
	return []string{
		"multi_ack",
		"thin-pack",
		"side-band",
		"side-band-64k",
		"ofs-delta",
		"shallow",
		"no-progress",
		"include-tag",
		"multi_ack_detailed",
		"no-done",
		"symref=HEAD:refs/heads/main",
		"agent=infinite-git/1.0",
	}
}

// ReadObject reads an object from the repository.
func (r *Repository) ReadObject(hash string) ([]byte, error) {
	return object.Read(r.gitDir, hash)
}

// ReadObjectFull reads an object from the repository with its header.
func (r *Repository) ReadObjectFull(hash string) ([]byte, error) {
	return object.ReadFull(r.gitDir, hash)
}

// WriteObject writes an object to the repository.
func (r *Repository) WriteObject(obj object.Object) (string, error) {
	return object.Write(r.gitDir, obj)
}

// UpdateRef updates a reference to point to a new object.
func (r *Repository) UpdateRef(ref, hash string) error {
	refPath := filepath.Join(r.gitDir, ref)
	refDir := filepath.Dir(refPath)

	// Create ref directory if needed
	if err := os.MkdirAll(refDir, 0755); err != nil {
		return fmt.Errorf("creating ref directory: %w", err)
	}

	// Write new hash
	if err := os.WriteFile(refPath, []byte(hash+"\n"), 0644); err != nil {
		return fmt.Errorf("updating ref: %w", err)
	}

	return nil
}

// GetObject reads and returns an object by hash.
func (r *Repository) GetObject(hash string) (io.ReadCloser, error) {
	objPath := filepath.Join(r.gitDir, "objects", hash[:2], hash[2:])

	file, err := os.Open(objPath)
	if err != nil {
		return nil, fmt.Errorf("opening object: %w", err)
	}

	return file, nil
}
