package object

// Blob represents a Git blob object (file content).
type Blob struct {
	Content []byte
}

// NewBlob creates a new blob object.
func NewBlob(content []byte) *Blob {
	return &Blob{Content: content}
}

// Type returns the object type.
func (b *Blob) Type() Type {
	return TypeBlob
}

// Serialize returns the blob content.
func (b *Blob) Serialize() []byte {
	return b.Content
}
