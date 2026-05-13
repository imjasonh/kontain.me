package object

import (
	"bytes"
	"fmt"
	"time"
)

// Commit represents a Git commit object.
type Commit struct {
	Tree       string    // SHA-1 hash of the tree object
	Parent     string    // SHA-1 hash of the parent commit (empty for initial commit)
	Author     string    // Author name and email
	AuthorDate time.Time // Author timestamp
	Committer  string    // Committer name and email
	CommitDate time.Time // Commit timestamp
	Message    string    // Commit message
}

// NewCommit creates a new commit object.
func NewCommit(tree, parent, author, committer, message string) *Commit {
	now := time.Now()
	return &Commit{
		Tree:       tree,
		Parent:     parent,
		Author:     author,
		AuthorDate: now,
		Committer:  committer,
		CommitDate: now,
		Message:    message,
	}
}

// Type returns the object type.
func (c *Commit) Type() Type {
	return TypeCommit
}

// Serialize returns the commit content in Git format.
func (c *Commit) Serialize() []byte {
	var buf bytes.Buffer

	// Tree reference
	fmt.Fprintf(&buf, "tree %s\n", c.Tree)

	// Parent reference (if exists)
	if c.Parent != "" {
		fmt.Fprintf(&buf, "parent %s\n", c.Parent)
	}

	// Author
	fmt.Fprintf(&buf, "author %s %d %s\n",
		c.Author,
		c.AuthorDate.Unix(),
		c.AuthorDate.Format("-0700"))

	// Committer
	fmt.Fprintf(&buf, "committer %s %d %s\n",
		c.Committer,
		c.CommitDate.Unix(),
		c.CommitDate.Format("-0700"))

	// Empty line before message
	buf.WriteByte('\n')

	// Commit message
	buf.WriteString(c.Message)

	// Ensure message ends with newline
	if len(c.Message) > 0 && c.Message[len(c.Message)-1] != '\n' {
		buf.WriteByte('\n')
	}

	return buf.Bytes()
}
