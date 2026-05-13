package main

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/imjasonh/kontain.me/internal/repo"
	"github.com/imjasonh/kontain.me/internal/server"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	content := &gitContent{}
	serverRepo, err := repo.New(t.TempDir(), content.InitialFiles())
	if err != nil {
		t.Fatalf("failed to create server repo: %v", err)
	}
	srv := server.New(serverRepo, content, "")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestCloneAndPull(t *testing.T) {
	ts := newTestServer(t)
	clientRepoDir := t.TempDir()

	// Clone the repository
	gitRepo, err := git.PlainClone(clientRepoDir, false, &git.CloneOptions{
		URL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	// Get initial commit
	ref, err := gitRepo.Head()
	if err != nil {
		t.Fatalf("failed to get HEAD: %v", err)
	}
	initialCommit := ref.Hash()
	t.Logf("Initial commit: %s", initialCommit)

	// Count initial commits
	initialCount := countCommits(t, gitRepo)
	t.Logf("Initial commit count: %d", initialCount)

	// Perform multiple pulls
	commits := []plumbing.Hash{initialCommit}
	for i := 0; i < 3; i++ {
		// Pull from origin
		w, err := gitRepo.Worktree()
		if err != nil {
			t.Fatalf("failed to get worktree: %v", err)
		}

		err = w.Pull(&git.PullOptions{
			RemoteName: "origin",
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			t.Fatalf("pull %d failed: %v", i+1, err)
		}

		// Get new HEAD
		ref, err := gitRepo.Head()
		if err != nil {
			t.Fatalf("failed to get HEAD after pull %d: %v", i+1, err)
		}
		newCommit := ref.Hash()

		// Verify we got a new commit
		if newCommit == commits[len(commits)-1] {
			t.Errorf("pull %d did not generate a new commit", i+1)
		}
		commits = append(commits, newCommit)
		t.Logf("Pull %d got commit: %s", i+1, newCommit)

		// Verify commit count increased
		newCount := countCommits(t, gitRepo)
		if newCount != initialCount+i+1 {
			t.Errorf("expected %d commits after pull %d, got %d", initialCount+i+1, i+1, newCount)
		}

		// Verify hello.txt exists and has been updated
		helloFile := filepath.Join(clientRepoDir, "hello.txt")
		content, err := os.ReadFile(helloFile)
		if err != nil {
			t.Errorf("failed to read hello.txt: %v", err)
		} else {
			// Clone increments counter to 1, so first pull is #2, second is #3, etc.
			expectedContent := fmt.Sprintf("Pull #%d\n", i+2)
			if !strings.Contains(string(content), expectedContent) {
				t.Errorf("hello.txt does not contain expected content %q, got: %s", expectedContent, content)
			}
		}
	}

	// Verify all commits are unique
	uniqueCommits := make(map[plumbing.Hash]bool)
	for _, commit := range commits {
		if uniqueCommits[commit] {
			t.Errorf("found duplicate commit: %s", commit)
		}
		uniqueCommits[commit] = true
	}
}

func TestConcurrentPulls(t *testing.T) {
	ts := newTestServer(t)

	// Clone initial repository
	baseClientDir := t.TempDir()
	baseRepo, err := git.PlainClone(baseClientDir, false, &git.CloneOptions{
		URL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	initialCount := countCommits(t, baseRepo)
	t.Logf("Initial commit count: %d", initialCount)

	// Perform concurrent pulls
	const numPulls = 5
	var wg sync.WaitGroup
	commits := make([]plumbing.Hash, numPulls)
	errors := make([]error, numPulls)

	for i := 0; i < numPulls; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Clone a fresh repo for this goroutine
			clientDir := filepath.Join(t.TempDir(), fmt.Sprintf("client_%d", idx))
			gitRepo, err := git.PlainClone(clientDir, false, &git.CloneOptions{
				URL: ts.URL,
			})
			if err != nil {
				errors[idx] = fmt.Errorf("clone failed: %w", err)
				return
			}

			// Get the commit we received
			ref, err := gitRepo.Head()
			if err != nil {
				errors[idx] = fmt.Errorf("get HEAD failed: %w", err)
				return
			}
			commits[idx] = ref.Hash()
			t.Logf("Concurrent pull %d got commit: %s", idx, commits[idx])
		}(i)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			t.Errorf("goroutine %d failed: %v", i, err)
		}
	}

	// Verify all commits are unique
	uniqueCommits := make(map[plumbing.Hash]bool)
	for i, commit := range commits {
		if uniqueCommits[commit] {
			t.Errorf("concurrent pull %d got duplicate commit: %s", i, commit)
		}
		uniqueCommits[commit] = true
	}

	// All pulls should have generated unique commits
	if len(uniqueCommits) != numPulls {
		t.Errorf("expected %d unique commits, got %d", numPulls, len(uniqueCommits))
	}
}

func TestPushRejection(t *testing.T) {
	ts := newTestServer(t)
	clientRepoDir := t.TempDir()

	// Clone the repository
	gitRepo, err := git.PlainClone(clientRepoDir, false, &git.CloneOptions{
		URL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	// Create a new file and commit
	testFile := filepath.Join(clientRepoDir, "test-push.txt")
	if err := os.WriteFile(testFile, []byte("test push content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	w, err := gitRepo.Worktree()
	if err != nil {
		t.Fatalf("failed to get worktree: %v", err)
	}

	if _, err := w.Add("test-push.txt"); err != nil {
		t.Fatalf("failed to add file: %v", err)
	}

	if _, err := w.Commit("Test push commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
		},
	}); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Try to push - should fail
	err = gitRepo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth: &http.BasicAuth{
			Username: "test",
			Password: "test",
		},
	})

	if err == nil {
		t.Fatal("push should have been rejected but succeeded")
	}

	// Verify the error indicates push was forbidden
	t.Logf("Push rejected with error: %v", err)
}

// Helper function to count commits
func countCommits(t *testing.T, repo *git.Repository) int {
	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		t.Fatalf("failed to get log: %v", err)
	}
	defer iter.Close()

	count := 0
	err = iter.ForEach(func(c *object.Commit) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("failed to iterate commits: %v", err)
	}
	return count
}
