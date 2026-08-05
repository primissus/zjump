package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/primissus/zjump/internal/db"
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

// TestSeedWorktrees_OnceOnly: seeding inserts a missing worktree with rank 1.0
// exactly once; a second seed does not inflate its rank (real visits are the
// only force that grows rank).
func TestSeedWorktrees_OnceOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repoDir := t.TempDir()
	gitCmd := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repoDir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}
	gitCmd("-c", "init.defaultBranch=main", "init")
	gitCmd("config", "user.email", "test@test.local")
	gitCmd("config", "user.name", "Test")
	gitCmd("commit", "--allow-empty", "-m", "init")

	wt2 := filepath.Join(filepath.Dir(repoDir), "wt2")
	gitCmd("worktree", "add", wt2, "-b", "feature")

	// Drop a directory entry for the main checkout into the DB (the worktree
	// paths themselves are not yet indexed).
	canonicalRepo, _ := filepath.EvalSymlinks(repoDir)
	canonicalWT, _ := filepath.EvalSymlinks(wt2)

	db := mustOpenTestDB(t)
	defer db.Save()
	const now uint64 = 1000
	db.AddUpdate(canonicalRepo, 1.0, now)

	// First seed: both worktrees are missing; both should be inserted at 1.0.
	seedWorktrees(db, canonicalRepo, now)
	rankOf := func(p string) float64 {
		for _, d := range db.Dirs() {
			if d.Path == p {
				return d.Rank
			}
		}
		return -1
	}
	if r := rankOf(canonicalRepo); r != 1.0 {
		t.Errorf("main rank after seed = %v, want 1.0", r)
	}
	if r := rankOf(canonicalWT); r != 1.0 {
		t.Errorf("worktree rank after seed = %v, want 1.0", r)
	}

	// Second seed: both already present; ranks must not change.
	seedWorktrees(db, canonicalRepo, now)
	if r := rankOf(canonicalRepo); r != 1.0 {
		t.Errorf("main rank after second seed = %v, want 1.0 (unchanged)", r)
	}
	if r := rankOf(canonicalWT); r != 1.0 {
		t.Errorf("worktree rank after second seed = %v, want 1.0 (unchanged)", r)
	}
}

// TestCollectAllReposWorktrees_Dedup verifies that a single repo reached through
// multiple tracked paths emits its worktrees exactly once, each labeled with a
// repo disambiguator.
func TestCollectAllReposWorktrees_Dedup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repoDir := t.TempDir()
	if out, err := runGitIn(repoDir, "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := runGitIn(repoDir, "config", "user.email", "t@e.st"); err != nil {
		t.Fatalf("git config user.email: %v\n%s", err, out)
	}
	if out, err := runGitIn(repoDir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("git config user.name: %v\n%s", err, out)
	}
	if out, err := runGitIn(repoDir, "commit", "--allow-empty", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	wt2 := filepath.Join(filepath.Dir(repoDir), "wt2-"+filepath.Base(repoDir))
	if out, err := runGitIn(repoDir, "worktree", "add", wt2, "-b", "feature"); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	canonicalRepo, _ := filepath.EvalSymlinks(repoDir)
	canonicalWT, _ := filepath.EvalSymlinks(wt2)

	db := mustOpenTestDB(t)
	defer db.Save()
	const now uint64 = 1000
	// Track BOTH the main checkout and the feature worktree as DB dirs, so the
	// dedup-by-canonical-main-checkout logic must collapse them to one repo.
	db.AddUpdate(canonicalRepo, 1.0, now)
	db.AddUpdate(canonicalWT, 1.0, now)

	entries, err := collectAllReposWorktrees(db, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("collectAllReposWorktrees = %d entries, want 2 (deduped): %+v", len(entries), entries)
	}
	paths := map[string]bool{}
	for _, e := range entries {
		paths[e.path] = true
		if !strings.Contains(e.label, "[repo: ") {
			t.Errorf("entry %+v missing repo disambiguator in label", e)
		}
	}
	if !paths[canonicalRepo] || !paths[canonicalWT] {
		t.Errorf("missing worktree paths; got %v", paths)
	}
}

// TestRunWorktreePickAll_Single: a single tracked repo with a single worktree
// takes the fast path and prints the path (no fzf), via `worktree --all`.
func TestRunWorktreePickAll_Single(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repoDir := t.TempDir()
	t.Setenv("_ZJUMP_DATA_DIR", t.TempDir())
	if out, err := runGitIn(repoDir, "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := runGitIn(repoDir, "config", "user.email", "t@e.st"); err != nil {
		t.Fatalf("git config user.email: %v\n%s", err, out)
	}
	if out, err := runGitIn(repoDir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("git config user.name: %v\n%s", err, out)
	}
	if out, err := runGitIn(repoDir, "commit", "--allow-empty", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	canonical, _ := filepath.EvalSymlinks(repoDir)
	if err := runAdd([]string{canonical}); err != nil {
		t.Fatalf("runAdd: %v", err)
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runWorktreePickAll(nil)
	w.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("runWorktreePickAll error: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != canonical {
		t.Errorf("runWorktreePickAll = %q, want %q", got, canonical)
	}
}

// mustOpenTestDB opens the database under a fresh temp data dir, pointing
// _ZJUMP_DATA_DIR at it for the duration of the test.
func mustOpenTestDB(t *testing.T) *db.Database {
	t.Helper()
	t.Setenv("_ZJUMP_DATA_DIR", t.TempDir())
	database, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	return database
}
