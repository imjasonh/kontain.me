package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	_ "github.com/chainguard-dev/clog/gcp/init"
	"github.com/imjasonh/kontain.me/internal/generator"
	"github.com/imjasonh/kontain.me/internal/repo"
	"github.com/imjasonh/kontain.me/internal/server"
	"github.com/sethvargo/go-envconfig"
)

var env = envconfig.MustProcess(context.Background(), &struct {
	Port     string `env:"PORT,default=8080"`
	RepoPath string `env:"REPO_PATH,default=/tmp/infinite-git-repo"`
}{})

// gitContent provides the default infinite-git file content.
type gitContent struct{}

func (g *gitContent) InitialFiles() map[string][]byte {
	return map[string][]byte{
		"README.md": []byte("# Infinite Git Repository\n\nThis repository generates a new commit every time you pull.\n"),
		"hello.txt": []byte("Pull #0\nTimestamp: Initial commit\n"),
	}
}

func (g *gitContent) GenerateFiles(count int64, now time.Time) map[string][]byte {
	return map[string][]byte{
		"hello.txt": []byte(fmt.Sprintf("Pull #%d\nTimestamp: %s\n", count, now.Format("2006-01-02 15:04:05.999999999"))),
	}
}

func (g *gitContent) CommitMessage(count int64, now time.Time) string {
	return fmt.Sprintf("Pull #%d at %s", count, now.Format("2006-01-02 15:04:05"))
}

var _ generator.ContentProvider = (*gitContent)(nil)

func main() {
	slog.Info("initializing repository", "env", env)
	content := &gitContent{}
	gitRepo, err := repo.New(env.RepoPath, content.InitialFiles())
	if err != nil {
		slog.Error("failed to initialize repository", "error", err)
		os.Exit(1)
	}

	srv := server.New(gitRepo, content, "https://github.com/imjasonh/kontain.me/blob/main/cmd/infinite-git")

	httpServer := &http.Server{
		Addr:         ":" + env.Port,
		Handler:      srv.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	slog.Info("starting HTTP server", "port", env.Port)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP server error", "error", err)
		os.Exit(1)
	}
}
