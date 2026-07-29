package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/primissus/zjump/internal/git"
)

func chdirTemp(t *testing.T) func() {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	os.Chdir(t.TempDir())
	return func() {
		os.Chdir(orig)
	}
}

func TestRunBranch_NoArg_NoPanic(t *testing.T) {
	t.Setenv("_ZJUMP_DATA_DIR", t.TempDir())
	defer chdirTemp(t)()

	err := runBranch(nil)
	if err == nil {
		t.Fatal("expected error when branch no-arg outside git repo with empty DB, got nil")
	}
	if !strings.Contains(err.Error(), "no git worktrees found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunWorktree_NoArg_NoPanic(t *testing.T) {
	t.Setenv("_ZJUMP_DATA_DIR", t.TempDir())
	defer chdirTemp(t)()

	err := runWorktree(nil)
	if err == nil {
		t.Fatal("expected error when worktree no-arg outside git repo with empty DB, got nil")
	}
	if !strings.Contains(err.Error(), "no git worktrees found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunBranch_NoArg_InRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	t.Setenv("_ZJUMP_DATA_DIR", t.TempDir())

	mainWT := filepath.Join(dir, "main")
	os.MkdirAll(mainWT, 0o755)
	gitCmd := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", mainWT}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}
	gitCmd("-c", "init.defaultBranch=main", "init")
	gitCmd("config", "user.email", "test@test.local")
	gitCmd("config", "user.name", "Test")
	gitCmd("commit", "--allow-empty", "-m", "init")

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)
	os.Chdir(mainWT)

	// Capture stdout for the single-offer fast path (1 branch → no fzf).
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runBranch(nil)
	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("runBranch(nil) error: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	want, _ := filepath.EvalSymlinks(mainWT)
	if got != want {
		t.Errorf("runBranch(nil) = %q, want %q", got, want)
	}
}

func TestRunWorktree_NoArg_InRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	t.Setenv("_ZJUMP_DATA_DIR", t.TempDir())

	mainWT := filepath.Join(dir, "main")
	os.MkdirAll(mainWT, 0o755)
	gitCmd := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", mainWT}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}
	gitCmd("-c", "init.defaultBranch=main", "init")
	gitCmd("config", "user.email", "test@test.local")
	gitCmd("config", "user.name", "Test")
	gitCmd("commit", "--allow-empty", "-m", "init")

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)
	os.Chdir(mainWT)

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runWorktree(nil)
	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("runWorktree(nil) error: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	want, _ := filepath.EvalSymlinks(mainWT)
	if got != want {
		t.Errorf("runWorktree(nil) = %q, want %q", got, want)
	}
}

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
