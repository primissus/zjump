package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/primissus/zjump/internal/git"
)

func TestGitFzfPickAndPrint_Single(t *testing.T) {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	entries := []gitEntry{{label: "main", path: "/tmp/repo"}}
	err := gitFzfPickAndPrint(entries)
	w.Close()

	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = orig

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "/tmp/repo" {
		t.Errorf("got %q, want /tmp/repo", got)
	}
}

func TestGitFzfPickAndPrint_Empty(t *testing.T) {
	err := gitFzfPickAndPrint(nil)
	if err == nil {
		t.Fatal("expected error for empty entries")
	}
	if err.Error() != "no matches found" {
		t.Errorf("got error %q, want 'no matches found'", err.Error())
	}
}

func TestCollectBranchEntries_Integration(t *testing.T) {
	cwd, _ := os.Getwd()
	repo, err := git.RepoRoot(cwd)
	if err != nil {
		t.Skipf("not in a git repo: %v", err)
	}
	entries, err := collectBranchEntries(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one branch entry (main branch)")
	}
	for _, e := range entries {
		if e.label == "" {
			t.Error("branch entry with empty label")
		}
		if e.path == "" {
			t.Error("branch entry with empty path")
		}
	}
}

func TestCollectWorktreeEntries_Integration(t *testing.T) {
	cwd, _ := os.Getwd()
	repo, err := git.RepoRoot(cwd)
	if err != nil {
		t.Skipf("not in a git repo: %v", err)
	}
	entries, err := collectWorktreeEntries(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one worktree entry (main)")
	}
	for _, e := range entries {
		if e.label == "" {
			t.Error("worktree entry with empty label")
		}
		if e.path == "" {
			t.Error("worktree entry with empty path")
		}
	}
}
