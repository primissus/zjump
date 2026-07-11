package gitx

import (
	"os"
	"path/filepath"
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
