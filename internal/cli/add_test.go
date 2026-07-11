package cli

import (
	"os"
	"path/filepath"
	"testing"

	"zjump/internal/db"
)

// kindOf returns the stored kind for path, or (0,false) if absent.
func kindOf(t *testing.T, path string) (db.Kind, bool) {
	t.Helper()
	database := openTestDB(t)
	for _, d := range database.Dirs() {
		if !d.IsAlias() && d.Path == path {
			return d.Kind, true
		}
	}
	return 0, false
}

// TestAddUpgradesDirToRepo: adding a plain directory stores KindDir; once it
// gains a .git marker, a subsequent add upgrades the entry to KindRepo in place
// (§2, R2-IDX-2).
func TestAddUpgradesDirToRepo(t *testing.T) {
	setupDataDir(t)
	dir := t.TempDir()

	if err := runAdd([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if k, ok := kindOf(t, dir); !ok || k != db.KindDir {
		t.Fatalf("first add: kind = %v present=%v, want KindDir", k, ok)
	}

	mkRepo(t, dir) // add .git
	if err := runAdd([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if k, ok := kindOf(t, dir); !ok || k != db.KindRepo {
		t.Errorf("after repo add: kind = %v present=%v, want KindRepo", k, ok)
	}
}

// TestAddNeverDowngradesRepo: a repo entry stays KindRepo even if .git later
// disappears and the path is re-added as a plain directory (§2, R2-IDX-2).
func TestAddNeverDowngradesRepo(t *testing.T) {
	setupDataDir(t)
	dir := t.TempDir()
	mkRepo(t, dir)

	if err := runAdd([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if k, _ := kindOf(t, dir); k != db.KindRepo {
		t.Fatalf("first add of a repo: kind = %v, want KindRepo", k)
	}

	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := runAdd([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if k, _ := kindOf(t, dir); k != db.KindRepo {
		t.Errorf("after re-adding as a plain dir: kind = %v, want KindRepo (never downgrade)", k)
	}
}
