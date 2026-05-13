package protocol

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/imjasonh/kontain.me/internal/object"
	"github.com/imjasonh/kontain.me/internal/packfile"
	"github.com/imjasonh/kontain.me/internal/pktline"
	"github.com/imjasonh/kontain.me/internal/repo"
)

// UploadPack implements the git-upload-pack protocol.
type UploadPack struct {
	repo *repo.Repository
}

// NewUploadPack creates a new upload-pack handler.
func NewUploadPack(r *repo.Repository) *UploadPack {
	return &UploadPack{repo: r}
}

// HandleRequest processes a git-upload-pack request.
func (u *UploadPack) HandleRequest(r io.Reader, w io.Writer) error {
	reader := pktline.NewReader(r)
	writer := pktline.NewWriter(w)

	// Read want lines first
	var wants []string
	var capabilities []string

	for {
		line, err := reader.ReadString()
		if err == io.EOF {
			break // flush-pkt
		}
		if err != nil {
			return fmt.Errorf("reading wants: %w", err)
		}

		if strings.HasPrefix(line, "want ") {
			wantLine := line[5:]
			// First want may have capabilities after space
			parts := strings.SplitN(wantLine, " ", 2)
			wants = append(wants, parts[0])

			// Parse capabilities if present
			if len(parts) > 1 && len(capabilities) == 0 {
				capabilities = strings.Split(parts[1], " ")
			}
		}
	}

	// Negotiation phase: the client sends "have" lines for objects it
	// already has, and we ACK the ones we recognize so git can compute a
	// minimal pack. With multi_ack_detailed we send "ACK <sha> common"
	// for each recognized object.
	var common []string

	for {
		var haves []string
		gotDone := false

		for {
			line, err := reader.ReadString()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("reading negotiation: %w", err)
			}

			if line == "done" {
				gotDone = true
				break
			} else if strings.HasPrefix(line, "have ") {
				haves = append(haves, line[5:])
			} else if line != "" {
				return fmt.Errorf("unexpected line in negotiation: %q", line)
			}
		}

		// ACK objects we have, NAK the rest. Process haves even when
		// "done" arrived in the same batch — the client needs ACKs
		// before we send the packfile.
		var lastAck string
		for _, have := range haves {
			if _, err := u.repo.ReadObjectFull(have); err == nil {
				common = append(common, have)
				lastAck = have
				if err := writer.WriteString(fmt.Sprintf("ACK %s common\n", have)); err != nil {
					return fmt.Errorf("writing ACK: %w", err)
				}
			}
		}

		if gotDone {
			break
		}

		if lastAck != "" {
			if err := writer.WriteString(fmt.Sprintf("ACK %s continue\n", lastAck)); err != nil {
				return fmt.Errorf("writing ACK continue: %w", err)
			}
		} else {
			if err := writer.WriteString("NAK\n"); err != nil {
				return fmt.Errorf("writing NAK: %w", err)
			}
		}
	}

	// Try to read the flush after "done". The client may or may not send
	// one, and the body may already be closed (especially behind proxies).
	if _, err := reader.ReadString(); err != nil && err != io.EOF {
		// Ignore read errors on closed body — the client is done sending.
	}

	// After "done", send a final ACK for the last common object (or NAK
	// if there are no common objects, e.g. initial clone).
	if len(common) > 0 {
		if err := writer.WriteString(fmt.Sprintf("ACK %s\n", common[len(common)-1])); err != nil {
			return fmt.Errorf("writing final ACK: %w", err)
		}
	} else {
		if err := writer.WriteString("NAK\n"); err != nil {
			return fmt.Errorf("writing final NAK: %w", err)
		}
	}

	// Check if client supports side-band
	sideBand := false
	for _, cap := range capabilities {
		if cap == "side-band" || cap == "side-band-64k" {
			sideBand = true
			break
		}
	}

	// Build a set of objects the client already has so we can skip them
	// (and their ancestors) when building the packfile.
	commonSet := make(map[string]bool, len(common))
	for _, c := range common {
		commonSet[c] = true
	}

	// Create and send packfile
	if sideBand {
		return u.sendPackfileWithSideband(writer, wants, commonSet)
	} else {
		return u.sendPackfile(w, wants, commonSet)
	}
}

// sendPackfile sends a packfile containing the requested objects.
func (u *UploadPack) sendPackfile(w io.Writer, wants []string, common map[string]bool) error {
	pack, err := u.createPackfile(wants, common)
	if err != nil {
		return fmt.Errorf("creating packfile: %w", err)
	}

	// Write packfile data directly (not as pkt-line)
	if _, err := w.Write(pack); err != nil {
		return fmt.Errorf("writing packfile: %w", err)
	}

	return nil
}

// sendPackfileWithSideband sends a packfile with sideband encoding.
func (u *UploadPack) sendPackfileWithSideband(w *pktline.Writer, wants []string, common map[string]bool) error {
	pack, err := u.createPackfile(wants, common)
	if err != nil {
		return fmt.Errorf("creating packfile: %w", err)
	}

	// Send packfile data in chunks with sideband 1 prefix
	const maxChunkSize = 65515 // Max pkt-line size minus sideband byte
	for i := 0; i < len(pack); i += maxChunkSize {
		end := i + maxChunkSize
		if end > len(pack) {
			end = len(pack)
		}

		chunk := append([]byte{1}, pack[i:end]...) // 1 = pack data channel
		if err := w.Write(chunk); err != nil {
			return fmt.Errorf("writing sideband chunk: %w", err)
		}
	}

	// Send flush packet to indicate end
	return w.Flush()
}

// createPackfile creates a packfile containing the requested objects and their
// dependencies, stopping at objects in the common set (already held by the client).
func (u *UploadPack) createPackfile(wants []string, common map[string]bool) ([]byte, error) {
	pw := packfile.NewWriter()
	visited := make(map[string]bool)

	// Mark common objects as already visited so we don't include them.
	for hash := range common {
		visited[hash] = true
	}

	for _, want := range wants {
		if err := u.addObjectToPack(pw, want, visited); err != nil {
			return nil, fmt.Errorf("adding object %s: %w", want, err)
		}
	}

	return pw.Finalize(), nil
}

// addObjectToPack recursively adds an object and its dependencies to the packfile.
func (u *UploadPack) addObjectToPack(pw *packfile.Writer, hash string, visited map[string]bool) error {
	if visited[hash] {
		return nil
	}
	visited[hash] = true

	// Read object with header
	data, err := u.repo.ReadObjectFull(hash)
	if err != nil {
		return fmt.Errorf("reading object: %w", err)
	}

	// Parse header
	nullIndex := bytes.IndexByte(data, 0)
	if nullIndex == -1 {
		return fmt.Errorf("invalid object format")
	}

	header := string(data[:nullIndex])
	content := data[nullIndex+1:]

	var objType int
	switch {
	case strings.HasPrefix(header, "commit "):
		objType = packfile.OBJ_COMMIT
		// Parse commit to find tree and parent
		if err := u.addCommitDependencies(pw, content, visited); err != nil {
			return err
		}
	case strings.HasPrefix(header, "tree "):
		objType = packfile.OBJ_TREE
		// Parse tree to find blobs and subtrees
		if err := u.addTreeDependencies(pw, content, visited); err != nil {
			return err
		}
	case strings.HasPrefix(header, "blob "):
		objType = packfile.OBJ_BLOB
		// Blobs have no dependencies
	default:
		return fmt.Errorf("unknown object type: %s", header)
	}

	// Add object to packfile
	return pw.AddObject(objType, content)
}

// addCommitDependencies adds a commit's tree and parent to the packfile.
func (u *UploadPack) addCommitDependencies(pw *packfile.Writer, commitData []byte, visited map[string]bool) error {
	lines := bytes.Split(commitData, []byte("\n"))
	for _, line := range lines {
		if bytes.HasPrefix(line, []byte("tree ")) {
			treeHash := string(line[5:])
			if err := u.addObjectToPack(pw, treeHash, visited); err != nil {
				return fmt.Errorf("adding tree: %w", err)
			}
		} else if bytes.HasPrefix(line, []byte("parent ")) {
			parentHash := string(line[7:])
			if err := u.addObjectToPack(pw, parentHash, visited); err != nil {
				return fmt.Errorf("adding parent: %w", err)
			}
		}
	}
	return nil
}

// addTreeDependencies adds a tree's entries to the packfile.
func (u *UploadPack) addTreeDependencies(pw *packfile.Writer, treeData []byte, visited map[string]bool) error {
	entries := parseTreeData(treeData)
	for _, entry := range entries {
		if err := u.addObjectToPack(pw, entry.Hash, visited); err != nil {
			return fmt.Errorf("adding tree entry %s: %w", entry.Name, err)
		}
	}
	return nil
}

// parseTreeData parses raw tree data into entries.
func parseTreeData(data []byte) []object.TreeEntry {
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
