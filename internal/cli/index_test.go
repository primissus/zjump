package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/primissus/zjump/internal/db"
)

// collectRepos runs the walk and returns the recorded repo paths (sorted).
func collectRepos(t *testing.T, root string, maxDepth int) []string {
	t.Helper()
	var found []string
	err := walkForRepos(root, 0, maxDepth, func(p string) error {
		found = append(found, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(found)
	return found
}

// TestIndexWalkDepthLimit: the walk descends only to maxDepth (root = depth 0),
// so a repo below that horizon is not found (§5.3, R2-IDX-3).
func TestIndexWalkDepthLimit(t *testing.T) {
	root := t.TempDir()
	shallow := filepath.Join(root, "shallow") // depth 1
	deep := filepath.Join(root, "x", "y", "deep")
	mkRepo(t, shallow)
	mkRepo(t, deep) // x=1, y=2, deep=3
	if err := os.MkdirAll(filepath.Join(root, "x", "y"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := collectRepos(t, root, 2)
	if len(got) != 1 || got[0] != shallow {
		t.Errorf("maxDepth=2 found %v, want only %q", got, shallow)
	}

	got = collectRepos(t, root, 3)
	want := []string{deep, shallow}
	sort.Strings(want)
	if len(got) != 2 {
		t.Fatalf("maxDepth=3 found %v, want both repos", got)
	}
}

// TestIndexSkipsHidden: a repo inside a hidden directory is not indexed (§5.3).
func TestIndexSkipsHidden(t *testing.T) {
	root := t.TempDir()
	visible := filepath.Join(root, "visible")
	hidden := filepath.Join(root, ".hidden", "repo")
	mkRepo(t, visible)
	mkRepo(t, hidden)

	got := collectRepos(t, root, 5)
	if len(got) != 1 || got[0] != visible {
		t.Errorf("found %v, want only the visible repo %q", got, visible)
	}
}

// TestIndexSkipsSymlinks: symlinked directories are never followed, so a repo
// reachable only through a symlink is not indexed (§5.3).
func TestIndexSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	mkRepo(t, actual)
	if err := os.Symlink(actual, filepath.Join(root, "mirror")); err != nil {
		t.Fatal(err)
	}

	got := collectRepos(t, root, 5)
	if len(got) != 1 || got[0] != actual {
		t.Errorf("found %v, want only the real repo %q (symlink not followed)", got, actual)
	}
}

// TestIndexNoDescentIntoRepo: once a repo root is recorded the walk does not
// descend, so a nested repo below it is not indexed (§5.3).
func TestIndexNoDescentIntoRepo(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	nested := filepath.Join(repo, "nested")
	mkRepo(t, repo)
	mkRepo(t, nested)

	got := collectRepos(t, root, 5)
	if len(got) != 1 || got[0] != repo {
		t.Errorf("found %v, want only the outer repo %q (no descent into repos)", got, repo)
	}
}

// TestIndexRespectsExcludeGlobs: a repo whose resolved path matches a
// _ZJUMP_EXCLUDE_DIRS glob is not added to the database (§5.3).
func TestIndexRespectsExcludeGlobs(t *testing.T) {
	setupDataDir(t)
	root := t.TempDir()
	keep := filepath.Join(root, "keep")
	skip := filepath.Join(root, "skip")
	mkRepo(t, keep)
	mkRepo(t, skip)

	t.Setenv("_ZJUMP_EXCLUDE_DIRS", filepath.Join(root, "skip"))

	if _, err := captureStderr(t, func() error { return runIndex([]string{root}) }); err != nil {
		t.Fatal(err)
	}

	database := openTestDB(t)
	paths := map[string]db.Kind{}
	for _, d := range database.Dirs() {
		paths[d.Path] = d.Kind
	}
	if _, ok := paths[skip]; ok {
		t.Errorf("excluded repo %q was indexed", skip)
	}
	if k, ok := paths[keep]; !ok || k != db.KindRepo {
		t.Errorf("kept repo %q missing or not KindRepo (got %v, present=%v)", keep, k, ok)
	}
}

// TestIndexUpsertsAsRepoAndSummarizes: a normal run stores each repo as KindRepo
// and reports the count on stderr (§5.3).
func TestIndexUpsertsAsRepoAndSummarizes(t *testing.T) {
	setupDataDir(t)
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "a"))
	mkRepo(t, filepath.Join(root, "b"))

	stderr, err := captureStderr(t, func() error { return runIndex([]string{root}) })
	if err != nil {
		t.Fatal(err)
	}
	if want := "indexed 2 repositories under 1 roots"; !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}

	database := openTestDB(t)
	if len(database.Dirs()) != 2 {
		t.Fatalf("want 2 entries, got %d", len(database.Dirs()))
	}
	for _, d := range database.Dirs() {
		if d.Kind != db.KindRepo {
			t.Errorf("entry %q kind = %d, want KindRepo", d.Path, d.Kind)
		}
	}
}

// TestIndexRejectsBadRoot: a nonexistent or non-directory root is an error,
// consistent with `add` (§5.3).
func TestIndexRejectsBadRoot(t *testing.T) {
	setupDataDir(t)
	if err := runIndex([]string{filepath.Join(t.TempDir(), "does-not-exist")}); err == nil {
		t.Error("expected an error for a nonexistent root")
	}
}

// TestAddUpgradesDirToRepo: adding a repo-rooted path that already exists as a
// dir entry upgrades it in place to KindRepo (rank/last_accessed preserved).
func TestAddUpgradesDirToRepo(t *testing.T) {
	setupDataDir(t)
	root := t.TempDir()
	mkRepo(t, root)

	database := openTestDB(t)
	database.AddUpdate(root, 2.0, 1000, db.KindDir)
	if err := database.Save(); err != nil {
		t.Fatal(err)
	}

	if err := runAdd([]string{root}); err != nil {
		t.Fatal(err)
	}

	database = openTestDB(t)
	dirs := database.Dirs()
	if len(dirs) != 1 {
		t.Fatalf("want 1 entry, got %d", len(dirs))
	}
	if dirs[0].Kind != db.KindRepo {
		t.Errorf("kind = %d, want KindRepo", dirs[0].Kind)
	}
	if dirs[0].Rank <= 2.0 {
		t.Errorf("rank = %v, want > 2.0 (incremented in place)", dirs[0].Rank)
	}
}

// TestAddNeverDowngradesRepo: adding a repo root that lost its .git still
// records it as KindRepo (a vanished .git leaves the typing; §2).
func TestAddNeverDowngradesRepo(t *testing.T) {
	setupDataDir(t)
	root := t.TempDir()

	// First add with a .git marker -> KindRepo.
	mkRepo(t, root)
	if err := runAdd([]string{root}); err != nil {
		t.Fatal(err)
	}
	// Remove the .git marker, then re-add the same path.
	if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := runAdd([]string{root}); err != nil {
		t.Fatal(err)
	}

	database := openTestDB(t)
	dirs := database.Dirs()
	if len(dirs) != 1 {
		t.Fatalf("want 1 entry, got %d", len(dirs))
	}
	if dirs[0].Kind != db.KindRepo {
		t.Errorf("kind = %d, want KindRepo (never downgrade)", dirs[0].Kind)
	}
}
