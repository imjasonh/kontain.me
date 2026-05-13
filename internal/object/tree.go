package object

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
)

// TreeEntry represents an entry in a Git tree object.
type TreeEntry struct {
	Mode string // File mode (e.g., "100644" for regular file)
	Name string // File or directory name
	Hash string // SHA-1 hash of the object
}

// Tree represents a Git tree object (directory listing).
type Tree struct {
	Entries []TreeEntry
}

// NewTree creates a new tree object.
func NewTree() *Tree {
	return &Tree{
		Entries: make([]TreeEntry, 0),
	}
}

// AddEntry adds an entry to the tree.
func (t *Tree) AddEntry(mode, name, hash string) {
	t.Entries = append(t.Entries, TreeEntry{
		Mode: mode,
		Name: name,
		Hash: hash,
	})
}

// Type returns the object type.
func (t *Tree) Type() Type {
	return TypeTree
}

// Serialize returns the tree content in Git format.
func (t *Tree) Serialize() []byte {
	// Sort entries by name for consistency
	sort.Slice(t.Entries, func(i, j int) bool {
		return t.Entries[i].Name < t.Entries[j].Name
	})

	var buf bytes.Buffer

	for _, entry := range t.Entries {
		// Format: <mode> <name>\0<20-byte SHA-1>
		fmt.Fprintf(&buf, "%s %s\x00", entry.Mode, entry.Name)

		// Convert hex hash to binary
		hashBytes, err := hex.DecodeString(entry.Hash)
		if err != nil {
			// This shouldn't happen with valid input
			panic(fmt.Sprintf("invalid hash: %s", entry.Hash))
		}
		buf.Write(hashBytes)
	}

	return buf.Bytes()
}
