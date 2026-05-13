package generator

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/imjasonh/kontain.me/internal/object"
	"github.com/imjasonh/kontain.me/internal/repo"
)

// Generator creates new commits on demand.
type Generator struct {
	repo     *repo.Repository
	counter  int64
	provider ContentProvider
}

// New creates a new commit generator.
func New(r *repo.Repository, provider ContentProvider) *Generator {
	return &Generator{
		repo:     r,
		provider: provider,
	}
}

// GenerateCommit creates a new commit and updates the main branch.
// It holds the repo lock for the entire read-modify-write cycle to
// prevent concurrent generates from reading the same parent.
func (g *Generator) GenerateCommit() (string, error) {
	// Increment counter atomically
	count := atomic.AddInt64(&g.counter, 1)

	// Hold the repo lock for the entire operation to prevent races.
	g.repo.Lock()
	defer g.repo.Unlock()

	// Get current HEAD commit (use exported method is fine since
	// getRefs is called internally, but we already hold the lock,
	// so we call the unexported version via GetRefsLocked).
	refs, err := g.repo.GetRefsLocked()
	if err != nil {
		return "", fmt.Errorf("getting refs: %w", err)
	}

	parentHash := refs["refs/heads/main"]
	if parentHash == "" {
		return "", fmt.Errorf("main branch not found")
	}

	// Read parent commit to get its tree
	parentData, err := g.repo.ReadObject(parentHash)
	if err != nil {
		return "", fmt.Errorf("reading parent commit: %w", err)
	}

	// Parse parent commit to find tree hash
	var parentTreeHash string
	lines := splitLines(string(parentData))
	for _, line := range lines {
		if len(line) > 5 && line[:5] == "tree " {
			parentTreeHash = line[5:]
			break
		}
	}

	// Read parent tree
	parentTreeData, err := g.repo.ReadObject(parentTreeHash)
	if err != nil {
		return "", fmt.Errorf("reading parent tree: %w", err)
	}

	// Parse existing tree entries
	existingEntries := parseTree(parentTreeData)

	// Generate files from content provider
	now := time.Now()
	generatedFiles := g.provider.GenerateFiles(count, now)

	// Create new tree with existing entries, replacing any generated files
	tree := object.NewTree()

	// Add existing entries, skipping any that will be replaced
	for _, entry := range existingEntries {
		if _, replaced := generatedFiles[entry.Name]; !replaced {
			tree.AddEntry(entry.Mode, entry.Name, entry.Hash)
		}
	}

	// Add generated files
	for name, content := range generatedFiles {
		blob := object.NewBlob(content)
		blobHash, err := g.repo.WriteObject(blob)
		if err != nil {
			return "", fmt.Errorf("writing blob for %s: %w", name, err)
		}
		tree.AddEntry("100644", name, blobHash)
	}

	treeHash, err := g.repo.WriteObject(tree)
	if err != nil {
		return "", fmt.Errorf("writing tree: %w", err)
	}

	// Create commit
	commitMsg := g.provider.CommitMessage(count, now)
	commit := object.NewCommit(
		treeHash,
		parentHash,
		"Infinite Git <infinite@example.com>",
		"Infinite Git <infinite@example.com>",
		commitMsg,
	)

	commitHash, err := g.repo.WriteObject(commit)
	if err != nil {
		return "", fmt.Errorf("writing commit: %w", err)
	}

	// Update refs/heads/main
	if err := g.repo.UpdateRef("refs/heads/main", commitHash); err != nil {
		return "", fmt.Errorf("updating ref: %w", err)
	}

	return commitHash, nil
}

// GetCounter returns the current counter value.
func (g *Generator) GetCounter() int64 {
	return atomic.LoadInt64(&g.counter)
}

// splitLines splits a string into lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// parseTree parses tree object data into entries.
func parseTree(data []byte) []object.TreeEntry {
	var entries []object.TreeEntry
	i := 0

	for i < len(data) {
		// Find space (end of mode)
		modeEnd := i
		for modeEnd < len(data) && data[modeEnd] != ' ' {
			modeEnd++
		}
		if modeEnd >= len(data) {
			break
		}
		mode := string(data[i:modeEnd])

		// Find null (end of name)
		nameStart := modeEnd + 1
		nameEnd := nameStart
		for nameEnd < len(data) && data[nameEnd] != 0 {
			nameEnd++
		}
		if nameEnd >= len(data) {
			break
		}
		name := string(data[nameStart:nameEnd])

		// Read 20-byte SHA-1
		hashStart := nameEnd + 1
		if hashStart+20 > len(data) {
			break
		}
		hash := fmt.Sprintf("%x", data[hashStart:hashStart+20])

		entries = append(entries, object.TreeEntry{
			Mode: mode,
			Name: name,
			Hash: hash,
		})

		i = hashStart + 20
	}

	return entries
}
