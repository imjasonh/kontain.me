package server

import (
	"net/http"
	"sync"

	"github.com/chainguard-dev/clog"
	"github.com/imjasonh/kontain.me/internal/generator"
	"github.com/imjasonh/kontain.me/internal/repo"
)

// Server handles Git HTTP protocol requests.
type Server struct {
	repo           *repo.Repository
	generator      *generator.Generator
	staticRedirect string
	mu             sync.Mutex
}

// New creates a new Git HTTP server. staticRedirect, if non-empty, is the URL
// that GET / 303-redirects to (matching the convention of the other kontain.me
// apps, which redirect the root to their source on GitHub).
func New(r *repo.Repository, provider generator.ContentProvider, staticRedirect string) *Server {
	return &Server{
		repo:           r,
		generator:      generator.New(r, provider),
		staticRedirect: staticRedirect,
	}
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Git smart HTTP endpoints
	mux.HandleFunc("/info/refs", s.handleInfoRefs)
	mux.HandleFunc("/git-upload-pack", s.handleUploadPack)
	mux.HandleFunc("/git-receive-pack", s.handleReceivePack)

	// Static file serving for dumb protocol (objects, refs)
	mux.HandleFunc("/", s.handleStatic)

	return s.logMiddleware(mux)
}

// logMiddleware logs HTTP requests.
func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := clog.FromContext(r.Context())
		log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"remote", r.RemoteAddr,
		)
		next.ServeHTTP(w, r)
	})
}

// handleReceivePack rejects push operations.
func (s *Server) handleReceivePack(w http.ResponseWriter, r *http.Request) {
	log := clog.FromContext(r.Context())
	log.Info("rejecting push attempt", "path", r.URL.Path)
	http.Error(w, "Push access denied", http.StatusForbidden)
}

// handleStatic redirects bare / to the configured source URL (or 404s anything
// else — only the smart Git protocol paths are otherwise handled).
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" && s.staticRedirect != "" {
		http.Redirect(w, r, s.staticRedirect, http.StatusSeeOther)
		return
	}
	http.NotFound(w, r)
}
