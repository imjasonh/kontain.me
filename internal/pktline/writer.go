package pktline

import (
	"fmt"
	"io"
)

// Writer implements the Git packet line protocol for writing.
type Writer struct {
	w io.Writer
}

// NewWriter creates a new packet line writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Write writes data as a pkt-line.
func (w *Writer) Write(data []byte) error {
	if len(data) == 0 {
		return w.Flush()
	}

	// Maximum pkt-line length is 65520 (65516 bytes of data + 4 bytes length)
	if len(data) > 65516 {
		return fmt.Errorf("pkt-line too long: %d bytes", len(data))
	}

	// Write 4-byte hex length prefix
	length := len(data) + 4
	header := fmt.Sprintf("%04x", length)
	if _, err := w.w.Write([]byte(header)); err != nil {
		return err
	}

	// Write data
	_, err := w.w.Write(data)
	return err
}

// WriteString writes a string as a pkt-line.
func (w *Writer) WriteString(s string) error {
	return w.Write([]byte(s))
}

// Writef writes formatted data as a pkt-line.
func (w *Writer) Writef(format string, args ...interface{}) error {
	return w.WriteString(fmt.Sprintf(format, args...))
}

// Flush writes a flush packet (0000).
func (w *Writer) Flush() error {
	_, err := w.w.Write([]byte("0000"))
	return err
}
