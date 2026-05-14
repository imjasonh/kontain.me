package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	srv := server.New(serverRepo, content, "")
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
	srv := server.New(serverRepo, content, "")
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

// TestUploadPackNoDoneTerminates verifies the handler doesn't hang when the
// client ends the request without "done". This is one round of stateless-RPC
// negotiation: the client sends haves and a flush, expecting just a NAK in
// response so it can decide what to send next. Sending pack data here would
// corrupt the HTTP keep-alive connection for the follow-up "done" round.
func TestUploadPackNoDoneTerminates(t *testing.T) {
	ts := newGoTestServer(t, "example.com/infinite-go")

	resp, err := http.Get(ts.URL + "/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	infoBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var sha string
	for i := 0; i+40 <= len(infoBody); i++ {
		ok := true
		for j := 0; j < 40; j++ {
			c := infoBody[i+j]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				ok = false
				break
			}
		}
		if ok && (i+40 == len(infoBody) || infoBody[i+40] == ' ') {
			sha = string(infoBody[i : i+40])
			break
		}
	}
	if sha == "" {
		t.Fatalf("could not parse SHA from info/refs")
	}

	pkt := func(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }
	// Request shaped exactly like a real git fetch using no-done: wants,
	// flush, haves, double flush, NO "done".
	req := pkt("want "+sha+" multi_ack_detailed no-done side-band-64k ofs-delta agent=test\n") +
		"0000" +
		pkt("have 1111111111111111111111111111111111111111\n") +
		pkt("have 2222222222222222222222222222222222222222\n") +
		"00000000"

	done := make(chan struct{})
	var body []byte
	var status int
	go func() {
		defer close(done)
		r, err := http.Post(ts.URL+"/git-upload-pack",
			"application/x-git-upload-pack-request", strings.NewReader(req))
		if err != nil {
			t.Errorf("POST: %v", err)
			return
		}
		body, _ = io.ReadAll(r.Body)
		r.Body.Close()
		status = r.StatusCode
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handler hung past 10s — no-done termination not handled")
	}
	if status != 200 {
		t.Fatalf("status %d, want 200; body: %q", status, body)
	}
	if string(body) != "0008NAK\n" {
		t.Errorf("want NAK-only response (no pack data); got %q", body)
	}
}

// TestUploadPackMultiBatchHaves drives the upload-pack handler directly with
// two flush-separated batches of haves the server doesn't know, then verifies
// that the response contains exactly one NAK (not two). Two NAKs would cause
// the client to interpret the second as the start of sideband-framed pack data
// and choke on band byte 'N' (0x4E=78) — "fetch-pack: protocol error: bad band #78".
func TestUploadPackMultiBatchHaves(t *testing.T) {
	ts := newGoTestServer(t, "example.com/infinite-go")

	resp, err := http.Get(ts.URL + "/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	infoBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var sha string
	for i := 0; i+40 <= len(infoBody); i++ {
		ok := true
		for j := 0; j < 40; j++ {
			c := infoBody[i+j]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				ok = false
				break
			}
		}
		if ok && (i+40 == len(infoBody) || infoBody[i+40] == ' ') {
			sha = string(infoBody[i : i+40])
			break
		}
	}
	if sha == "" {
		t.Fatalf("could not parse SHA from info/refs response")
	}

	pkt := func(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }
	req := pkt("want "+sha+" multi_ack_detailed side-band-64k ofs-delta agent=test\n") +
		"0000" +
		pkt("have 1111111111111111111111111111111111111111\n") +
		"0000" +
		pkt("have 2222222222222222222222222222222222222222\n") +
		pkt("done\n")

	resp, err = http.Post(ts.URL+"/git-upload-pack",
		"application/x-git-upload-pack-request", strings.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	naks := 0
	off := 0
	for off+4 <= len(body) {
		var length int
		fmt.Sscanf(string(body[off:off+4]), "%04x", &length)
		if length == 0 {
			off += 4
			continue
		}
		end := off + length
		if end > len(body) {
			break
		}
		content := body[off+4 : end]
		if string(content) == "NAK\n" {
			naks++
		}
		if len(content) > 0 && content[0] == 0x01 {
			break // entered sideband pack stream
		}
		off = end
	}
	if naks != 1 {
		t.Errorf("expected exactly 1 NAK before pack data, got %d", naks)
	}
}

// TestFetchWithUnknownHaves simulates a client whose local cache contains
// commits the server has never seen — e.g. a fresh Cloud Run instance after a
// previous instance generated different commits. The client sends "have" lines
// the server can't ACK; the server must still serve a valid sideband-framed
// packfile rather than confusing the client (previously: "bad band #78").
func TestFetchWithUnknownHaves(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary not found in PATH")
	}

	// Clone from a first server, then fetch from a second (fresh) server.
	// The client's cache will contain commits the second server has never
	// seen — this is the case that triggers "bad band #78".
	ts1 := newGoTestServer(t, "example.com/infinite-go")

	cacheDir := t.TempDir()
	cmd := exec.Command(gitBin, "init", "--bare", "-b", "main", cacheDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, append([]string{"-C", cacheDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("remote", "add", "origin", ts1.URL)
	// Build up enough history that the client sends "have" lines in
	// multiple flush-separated rounds on the next fetch — closer to a
	// real Cloud Run instance restart after many pulls.
	for i := 0; i < 3; i++ {
		runGit("fetch", "origin", "+refs/heads/*:refs/remotes/origin/*")
	}

	// Now point at a fresh server with no shared history and fetch again.
	ts2 := newGoTestServer(t, "example.com/infinite-go")
	runGit("remote", "set-url", "origin", ts2.URL)

	cmd = exec.Command(gitBin, "-C", cacheDir, "fetch", "-f", "--end-of-options",
		"origin", "refs/heads/*:refs/heads/*", "refs/tags/*:refs/tags/*")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fetch failed: %v\n%s", err, out)
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
