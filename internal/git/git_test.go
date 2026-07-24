package git

import (
	"strings"
	"testing"
)

func TestParseWorktrees(t *testing.T) {
	input := strings.TrimSpace(`
worktree /home/user/src/repo
HEAD abc1234
branch refs/heads/main

worktree /home/user/src/repo-feat
HEAD def5678
branch refs/heads/feature/api

worktree /home/user/src/repo-detach
HEAD 999aaaa
detached
`)
	wts, err := parseWorktrees(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 3 {
		t.Fatalf("got %d worktrees, want 3", len(wts))
	}

	// Main worktree.
	if wts[0].Path != "/home/user/src/repo" {
		t.Errorf("wt[0].Path = %q", wts[0].Path)
	}
	if wts[0].Branch != "main" {
		t.Errorf("wt[0].Branch = %q, want main", wts[0].Branch)
	}
	if wts[0].Detached {
		t.Error("wt[0] should not be detached")
	}

	// Feature branch.
	if wts[1].Branch != "feature/api" {
		t.Errorf("wt[1].Branch = %q", wts[1].Branch)
	}
	if wts[1].Path != "/home/user/src/repo-feat" {
		t.Errorf("wt[1].Path = %q", wts[1].Path)
	}

	// Detached.
	if !wts[2].Detached {
		t.Error("wt[2] should be detached")
	}
	if wts[2].Head != "999aaaa" {
		t.Errorf("wt[2].Head = %q", wts[2].Head)
	}
	if wts[2].Branch != "" {
		t.Errorf("wt[2].Branch = %q, want empty", wts[2].Branch)
	}
}

func TestParseWorktreesEmpty(t *testing.T) {
	wts, err := parseWorktrees("")
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 0 {
		t.Fatalf("got %d worktrees, want 0", len(wts))
	}
}

func TestParseWorktreesTrailingNewline(t *testing.T) {
	input := "worktree /a\nHEAD x\nbranch refs/heads/foo\n\n"
	wts, err := parseWorktrees(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 1 {
		t.Fatalf("got %d worktrees, want 1", len(wts))
	}
	if wts[0].Branch != "foo" {
		t.Errorf("Branch = %q", wts[0].Branch)
	}
}
