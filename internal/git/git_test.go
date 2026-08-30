package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepoRootDetectsGitDirAndGitFile: a path is a repo root when "<path>/.git"
// is either a directory (normal clone) or a regular file (linked worktree root),
// and not otherwise (R2-IDX-1).
func TestRepoRootDetectsGitDirAndGitFile(t *testing.T) {
	t.Run("git dir", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if !IsRepoRoot(root) {
			t.Error("expected a directory .git to be detected as a repo root")
		}
	})

	t.Run("git file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /somewhere\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !IsRepoRoot(root) {
			t.Error("expected a regular-file .git (worktree root) to be detected")
		}
	})

	t.Run("no git", func(t *testing.T) {
		root := t.TempDir()
		if IsRepoRoot(root) {
			t.Error("a directory without .git must not be a repo root")
		}
	})

	t.Run("git symlink is not followed", func(t *testing.T) {
		root := t.TempDir()
		target := t.TempDir()
		if err := os.Mkdir(filepath.Join(target, "realgit"), 0o755); err != nil {
			t.Fatal(err)
		}
		// A .git that is a symlink is neither a dir nor a regular file by Lstat,
		// so it must not count as a repo root.
		if err := os.Symlink(filepath.Join(target, "realgit"), filepath.Join(root, ".git")); err != nil {
			t.Fatal(err)
		}
		if IsRepoRoot(root) {
			t.Error("a symlink .git must not be treated as a repo root (Lstat, no follow)")
		}
	})
}

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

// TestPorcelainParser exercises the fixture cases from §6 step 3: a normal
// branch worktree, a detached one, a bare block (Bare set), and lines that must
// be ignored (HEAD/locked/prunable), plus multiple blocks (R2-WT-1).
func TestPorcelainParser(t *testing.T) {
	fixture := "" +
		"worktree /home/u/main\n" +
		"HEAD 1111111111111111111111111111111111111111\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /home/u/feature\n" +
		"HEAD 2222222222222222222222222222222222222222\n" +
		"branch refs/heads/feature\n" +
		"locked\n" +
		"\n" +
		"worktree /home/u/detached\n" +
		"HEAD 3333333333333333333333333333333333333333\n" +
		"detached\n" +
		"\n" +
		"worktree /home/u/bare-repo\n" +
		"bare\n" +
		"\n" +
		"worktree /home/u/prunable\n" +
		"HEAD 4444444444444444444444444444444444444444\n" +
		"branch refs/heads/gone\n" +
		"prunable gitdir file points to non-existent location\n"

	wts, err := parseWorktrees(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 5 {
		t.Fatalf("parsed %d worktrees, want 5: %+v", len(wts), wts)
	}

	// main: normal branch.
	if wts[0].Path != "/home/u/main" || wts[0].Branch != "main" || wts[0].Detached || wts[0].Bare {
		t.Errorf("main = %+v, want path /home/u/main branch main", wts[0])
	}
	// feature: locked line ignored.
	if wts[1].Path != "/home/u/feature" || wts[1].Branch != "feature" {
		t.Errorf("feature = %+v, want branch feature (locked ignored)", wts[1])
	}
	// detached.
	if !wts[2].Detached || wts[2].Path != "/home/u/detached" {
		t.Errorf("detached = %+v, want detached /home/u/detached", wts[2])
	}
	// bare: Bare flag set (skipped later at enumeration time).
	if !wts[3].Bare || wts[3].Path != "/home/u/bare-repo" {
		t.Errorf("bare = %+v, want Bare /home/u/bare-repo", wts[3])
	}
	// prunable line ignored.
	if wts[4].Path != "/home/u/prunable" || wts[4].Branch != "gone" {
		t.Errorf("prunable = %+v, want branch gone (prunable ignored)", wts[4])
	}
}
