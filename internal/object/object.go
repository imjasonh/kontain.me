package object

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Type represents a Git object type.
type Type string

const (
	TypeBlob   Type = "blob"
	TypeTree   Type = "tree"
	TypeCommit Type = "commit"
)

// Object represents a Git object.
type Object interface {
	Type() Type
	Serialize() []byte
}

// Hash computes the SHA-1 hash of an object.
func Hash(obj Object) string {
	data := obj.Serialize()
	header := fmt.Sprintf("%s %d\x00", obj.Type(), len(data))

	h := sha1.New()
	h.Write([]byte(header))
	h.Write(data)

	return fmt.Sprintf("%x", h.Sum(nil))
}

// Write writes an object to the Git object store.
func Write(gitDir string, obj Object) (string, error) {
	// Compute hash
	hash := Hash(obj)

	// Prepare object data
	data := obj.Serialize()
	header := fmt.Sprintf("%s %d\x00", obj.Type(), len(data))

	// Create object directory
	objDir := filepath.Join(gitDir, "objects", hash[:2])
	if err := os.MkdirAll(objDir, 0755); err != nil {
		return "", fmt.Errorf("creating object dir: %w", err)
	}

	// Write compressed object
	objPath := filepath.Join(objDir, hash[2:])
	file, err := os.Create(objPath)
	if err != nil {
		return "", fmt.Errorf("creating object file: %w", err)
	}
	defer file.Close()

	// Compress with zlib
	w := zlib.NewWriter(file)
	defer w.Close()

	if _, err := w.Write([]byte(header)); err != nil {
		return "", fmt.Errorf("writing header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return "", fmt.Errorf("writing data: %w", err)
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("closing zlib writer: %w", err)
	}

	return hash, nil
}

// ReadFull reads an object from the Git object store with its header.
func ReadFull(gitDir string, hash string) ([]byte, error) {
	objPath := filepath.Join(gitDir, "objects", hash[:2], hash[2:])

	file, err := os.Open(objPath)
	if err != nil {
		return nil, fmt.Errorf("opening object file: %w", err)
	}
	defer file.Close()

	// Decompress
	r, err := zlib.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("creating zlib reader: %w", err)
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading object: %w", err)
	}

	return data, nil
}

// Read reads an object from the Git object store.
func Read(gitDir string, hash string) ([]byte, error) {
	objPath := filepath.Join(gitDir, "objects", hash[:2], hash[2:])

	file, err := os.Open(objPath)
	if err != nil {
		return nil, fmt.Errorf("opening object file: %w", err)
	}
	defer file.Close()

	// Decompress
	r, err := zlib.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("creating zlib reader: %w", err)
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading object: %w", err)
	}

	// Parse header
	nullIndex := bytes.IndexByte(data, 0)
	if nullIndex == -1 {
		return nil, fmt.Errorf("invalid object format: no null byte")
	}

	// Return content after header
	return data[nullIndex+1:], nil
}
