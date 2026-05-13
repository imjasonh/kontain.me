package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/chainguard-dev/clog"
	"github.com/imjasonh/kontain.me/internal/pktline"
	"github.com/imjasonh/kontain.me/internal/protocol"
)

// handleInfoRefs handles the reference discovery phase.
func (s *Server) handleInfoRefs(w http.ResponseWriter, r *http.Request) {
	log := clog.FromContext(r.Context())
	service := r.URL.Query().Get("service")

	// Only support git-upload-pack (fetch/clone)
	if service != "git-upload-pack" {
		http.Error(w, "Service not supported", http.StatusForbidden)
		return
	}

	// Generate a new commit before advertising refs
	commitSHA, err := s.generator.GenerateCommit()

	if err != nil {
		log.Error("failed to generate commit", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Info("generated new commit", "sha", commitSHA, "counter", s.generator.GetCounter())

	// Set headers
	w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", service))
	w.Header().Set("Cache-Control", "no-cache")

	// Write response
	pw := pktline.NewWriter(w)

	// Service declaration
	if err := pw.Writef("# service=%s\n", service); err != nil {
		log.Error("failed to write service line", "error", err)
		return
	}
	if err := pw.Flush(); err != nil {
		log.Error("failed to write flush", "error", err)
		return
	}

	// Use the commitSHA directly from GenerateCommit rather than re-reading
	// refs. This avoids a race where concurrent requests could all see the
	// same latest ref, and ensures HEAD is always advertised first.
	capabilities := strings.Join(s.repo.GetCapabilities(), " ")

	// Advertise HEAD first (Git protocol requirement), then refs/heads/main.
	if err := pw.Writef("%s HEAD\x00%s\n", commitSHA, capabilities); err != nil {
		log.Error("failed to write HEAD ref", "error", err)
		return
	}
	if err := pw.Writef("%s refs/heads/main\n", commitSHA); err != nil {
		log.Error("failed to write main ref", "error", err)
		return
	}

	// Final flush
	if err := pw.Flush(); err != nil {
		log.Error("failed to write final flush", "error", err)
		return
	}
}

// handleUploadPack handles the pack upload phase.
func (s *Server) handleUploadPack(w http.ResponseWriter, r *http.Request) {
	log := clog.FromContext(r.Context())
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set headers
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	w.Header().Set("Cache-Control", "no-cache")

	// Create upload-pack handler
	up := protocol.NewUploadPack(s.repo)

	// Process the request
	if err := up.HandleRequest(r.Body, w); err != nil {
		log.Error("upload-pack failed", "error", err)
		// Don't send HTTP error here as we may have already started writing response
		return
	}

	log.Info("completed upload-pack")
}
