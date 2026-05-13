package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	_ "github.com/chainguard-dev/clog/gcp/init"
	"github.com/imjasonh/kontain.me/internal/generator"
	"github.com/imjasonh/kontain.me/internal/repo"
	"github.com/imjasonh/kontain.me/internal/server"
	"github.com/sethvargo/go-envconfig"
)

var env = envconfig.MustProcess(context.Background(), &struct {
	Port       string `env:"PORT,default=8080"`
	RepoPath   string `env:"REPO_PATH,default=/tmp/infinite-go-repo"`
	ModulePath string `env:"MODULE_PATH,default=infinite-go.kontain.me"`
}{})

// goContent generates a Go package with PullTime set to the pull timestamp.
type goContent struct {
	modulePath string
	pkgName    string
}

func newGoContent(modulePath string) *goContent {
	// Derive package name from last path element, removing hyphens.
	pkg := path.Base(modulePath)
	pkg = strings.ReplaceAll(pkg, "-", "")
	pkg = strings.ReplaceAll(pkg, ".", "")
	return &goContent{
		modulePath: modulePath,
		pkgName:    pkg,
	}
}

//go:embed LICENSE
var license []byte

// InitialFiles seeds the repo with the generated files plus a LICENSE so
// pkg.go.dev can display it. LICENSE isn't in GenerateFiles since it doesn't
// change, and the generator preserves existing files not in the map.
func (g *goContent) InitialFiles() map[string][]byte {
	files := g.GenerateFiles(0, time.Now())
	files["LICENSE"] = license
	return files
}

func (g *goContent) GenerateFiles(count int64, now time.Time) map[string][]byte {
	now = now.UTC()

	goMod := fmt.Sprintf("module %s\n\ngo 1.24\n", g.modulePath)

	goFile := fmt.Sprintf(`package %s

import "time"

// PullTime is the time this module version was generated.
var PullTime = time.Date(%d, time.%s, %d, %d, %d, %d, %d, time.UTC)
`,
		g.pkgName,
		now.Year(), now.Month(), now.Day(),
		now.Hour(), now.Minute(), now.Second(), now.Nanosecond(),
	)

	return map[string][]byte{
		"go.mod":       []byte(goMod),
		"pulltime.go":  []byte(goFile),
	}
}

func (g *goContent) CommitMessage(count int64, now time.Time) string {
	return fmt.Sprintf("Pull #%d at %s", count, now.Format("2006-01-02 15:04:05"))
}

var _ generator.ContentProvider = (*goContent)(nil)

// goGetMiddleware intercepts ?go-get=1 requests for Go module discovery.
func goGetMiddleware(modulePath string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("go-get") == "1" {
			// Cloud Run terminates TLS at the edge and proxies plain HTTP to
			// the container, so r.TLS is always nil; trust X-Forwarded-Proto.
			scheme := r.Header.Get("X-Forwarded-Proto")
			if scheme == "" {
				scheme = "http"
				if r.TLS != nil {
					scheme = "https"
				}
			}
			repoURL := fmt.Sprintf("%s://%s", scheme, r.Host)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html>
<html><head>
<meta name="go-import" content="%s git %s">
</head></html>
`, modulePath, repoURL)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	slog.Info("initializing repository", "env", env)
	content := newGoContent(env.ModulePath)
	gitRepo, err := repo.New(env.RepoPath, content.InitialFiles())
	if err != nil {
		slog.Error("failed to initialize repository", "error", err)
		os.Exit(1)
	}

	srv := server.New(gitRepo, content, "https://github.com/imjasonh/kontain.me/blob/main/cmd/infinite-go")
	handler := goGetMiddleware(env.ModulePath, srv.Handler())

	httpServer := &http.Server{
		Addr:         ":" + env.Port,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	slog.Info("starting HTTP server", "port", env.Port, "module", env.ModulePath)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP server error", "error", err)
		os.Exit(1)
	}
}
