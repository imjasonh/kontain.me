package pktline

import (
	"bufio"
	"fmt"
	"io"
)

// Reader implements the Git packet line protocol for reading.
type Reader struct {
	r *bufio.Reader
}

// NewReader creates a new packet line reader.
func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReader(r)}
}

// Read reads a single pkt-line.
// Returns io.EOF on flush packet (0000).
func (r *Reader) Read() ([]byte, error) {
	// Read 4-byte length header
	header := make([]byte, 4)
	if _, err := io.ReadFull(r.r, header); err != nil {
		return nil, err
	}

	// Parse length
	var length int
	if _, err := fmt.Sscanf(string(header), "%04x", &length); err != nil {
		return nil, fmt.Errorf("invalid pkt-line header: %s", header)
	}

	// Handle special packets
	switch length {
	case 0: // flush-pkt
		return nil, io.EOF
	case 1: // delimiter packet (0001)
		return nil, fmt.Errorf("delimiter packet not supported")
	case 2: // response-end packet (0002)
		return nil, fmt.Errorf("response-end packet not supported")
	}

	// Read data
	if length < 4 {
		return nil, fmt.Errorf("invalid pkt-line length: %d", length)
	}

	data := make([]byte, length-4)
	if _, err := io.ReadFull(r.r, data); err != nil {
		return nil, err
	}

	return data, nil
}

// ReadString reads a pkt-line as a string, trimming newline.
func (r *Reader) ReadString() (string, error) {
	data, err := r.Read()
	if err != nil {
		return "", err
	}

	// Trim trailing newline if present
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}

	return string(data), nil
}

// ReadAll reads all pkt-lines until flush packet.
func (r *Reader) ReadAll() ([][]byte, error) {
	var lines [][]byte

	for {
		line, err := r.Read()
		if err == io.EOF {
			// Flush packet, we're done
			break
		}
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}

	return lines, nil
}
