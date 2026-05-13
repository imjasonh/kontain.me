package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imjasonh/kontain.me/internal/repo"
	"github.com/imjasonh/kontain.me/internal/server"
)

func newGoTestServer(t *testing.T, modulePath string) *httptest.Server {
	t.Helper()
	content := newGoContent(modulePath)
	serverRepo, err := repo.New(t.TempDir(), content.InitialFiles())
	if err != nil {
		t.Fatalf("failed to create server repo: %v", err)
	}
	srv := server.New(serverRepo, content)
	ts := httptest.NewServer(goGetMiddleware(modulePath, srv.Handler()))
	t.Cleanup(ts.Close)
	return ts
}

// newGoTestServerOnPort starts a test server bound to 127.0.0.1:port.
// It skips the test if the port cannot be bound (e.g. no permission for :80).
func newGoTestServerOnPort(t *testing.T, modulePath string, port int) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("cannot bind to :%d: %v", port, err)
	}
	content := newGoContent(modulePath)
	serverRepo, err := repo.New(t.TempDir(), content.InitialFiles())
	if err != nil {
		t.Fatalf("failed to create server repo: %v", err)
	}
	srv := server.New(serverRepo, content)
	ts := httptest.NewUnstartedServer(goGetMiddleware(modulePath, srv.Handler()))
	ts.Listener = ln
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

// TestGoGetE2E runs `go get 127.0.0.1@latest` against the server on :80.
// This is the full end-to-end test of the Go module workflow.
// Skipped when :80 cannot be bound (requires CAP_NET_BIND_SERVICE or root).
func TestGoGetE2E(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not found in PATH")
	}

	modulePath := "127.0.0.1"
	newGoTestServerOnPort(t, modulePath, 80)

	var pullTimes []string

	for i := 0; i < 3; i++ {
		modCache := t.TempDir()
		workDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test\n\ngo 1.24\n"), 0644); err != nil {
			t.Fatalf("iteration %d: failed to write go.mod: %v", i, err)
		}
		if err := os.WriteFile(filepath.Join(workDir, "tools.go"), []byte(fmt.Sprintf("package tools\n\nimport _ \"%s\"\n", modulePath)), 0644); err != nil {
			t.Fatalf("iteration %d: failed to write tools.go: %v", i, err)
		}

		cmd := exec.Command(goBin, "get", modulePath+"@latest")
		cmd.Dir = workDir
		cmd.Env = append(os.Environ(),
			"GOMODCACHE="+modCache,
			"GOPROXY=direct",
			"GONOSUMCHECK=*",
			"GONOSUMDB=*",
			"GOINSECURE="+modulePath,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go get #%d failed: %v\noutput: %s", i+1, err, out)
		}
		t.Logf("go get #%d succeeded", i+1)

		// Find pulltime.go in the module cache.
		var pulltimeFile string
		filepath.Walk(modCache, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Name() == "pulltime.go" {
				pulltimeFile = path
				return filepath.SkipAll
			}
			return nil
		})
		if pulltimeFile == "" {
			t.Fatalf("go get #%d: pulltime.go not found in module cache", i+1)
		}

		data, err := os.ReadFile(pulltimeFile)
		if err != nil {
			t.Fatalf("go get #%d: failed to read pulltime.go: %v", i+1, err)
		}

		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "var PullTime") {
				pullTimes = append(pullTimes, strings.TrimSpace(line))
				t.Logf("go get #%d: %s", i+1, strings.TrimSpace(line))
				break
			}
		}
	}

	if len(pullTimes) != 3 {
		t.Fatalf("expected 3 PullTime values, got %d", len(pullTimes))
	}

	seen := make(map[string]bool)
	for i, pt := range pullTimes {
		if seen[pt] {
			t.Errorf("go get #%d returned duplicate PullTime: %s", i+1, pt)
		}
		seen[pt] = true
	}
}

// TestGoGetPull clones and then pulls repeatedly using the git CLI,
// verifying each pull produces a buildable Go package with a unique PullTime.
// This always runs (no privileged port needed) as a fallback for environments
// where TestGoGetE2E is skipped.
func TestGoGetPull(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not found in PATH")
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary not found in PATH")
	}

	modulePath := "example.com/infinite-go"
	ts := newGoTestServer(t, modulePath)

	cloneDir := t.TempDir()

	// Initial clone.
	cmd := exec.Command(gitBin, "clone", ts.URL, cloneDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone failed: %v\noutput: %s", err, out)
	}

	readPullTime := func() string {
		data, err := os.ReadFile(filepath.Join(cloneDir, "pulltime.go"))
		if err != nil {
			t.Fatalf("failed to read pulltime.go: %v", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "var PullTime") {
				return strings.TrimSpace(line)
			}
		}
		t.Fatal("pulltime.go does not contain PullTime declaration")
		return ""
	}

	prev := readPullTime()
	t.Logf("clone: %s", prev)

	for i := 0; i < 3; i++ {
		cmd := exec.Command(gitBin, "-C", cloneDir, "pull")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git pull #%d failed: %v\noutput: %s", i+1, err, out)
		}

		// Verify it still builds.
		cmd = exec.Command(goBin, "build", "./...")
		cmd.Dir = cloneDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pull #%d: go build failed: %v\noutput: %s", i+1, err, out)
		}

		cur := readPullTime()
		t.Logf("pull #%d: %s", i+1, cur)
		if cur == prev {
			t.Errorf("pull #%d did not change PullTime", i+1)
		}
		prev = cur
	}
}

func TestGoGetDiscovery(t *testing.T) {
	modulePath := "example.com/infinite-go"
	ts := newGoTestServer(t, modulePath)

	resp, err := http.Get(ts.URL + "/?go-get=1")
	if err != nil {
		t.Fatalf("failed to fetch go-get: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	expected := fmt.Sprintf(`<meta name="go-import" content="%s git`, modulePath)
	if !strings.Contains(body, expected) {
		t.Errorf("go-get response missing expected meta tag.\nwant substring: %s\ngot: %s", expected, body)
	}
}
