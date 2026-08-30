package db

import (
	"math"
	"testing"
)

const testEpoch Epoch = 946684800 // 2000-01-01, matches zoxide's fixture

// TestAddRoundTrip mirrors zoxide's db add test: two add_updates on the same
// path accumulate rank and round-trip through save+reopen (A-4, R-ADD-1).
func TestAddRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const path = "/foo/bar"

	{
		db, err := OpenDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		db.AddUpdate(path, 1.0, testEpoch, KindDir)
		db.AddUpdate(path, 1.0, testEpoch, KindDir)
		if err := db.Save(); err != nil {
			t.Fatal(err)
		}
	}
	{
		db, err := OpenDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(db.Dirs()) != 1 {
			t.Fatalf("want 1 entry, got %d", len(db.Dirs()))
		}
		d := db.Dirs()[0]
		if d.Path != path {
			t.Errorf("path = %q, want %q", d.Path, path)
		}
		if math.Abs(d.Rank-2.0) > 0.01 {
			t.Errorf("rank = %v, want 2.0", d.Rank)
		}
		if d.LastAccessed != testEpoch {
			t.Errorf("last_accessed = %d, want %d", d.LastAccessed, testEpoch)
		}
	}
}

// TestRemoveRoundTrip mirrors zoxide's remove test.
func TestRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const path = "/foo/bar"

	db, _ := OpenDir(dir)
	db.AddUpdate(path, 1.0, testEpoch, KindDir)
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}

	db, _ = OpenDir(dir)
	if !db.Remove(path) {
		t.Error("Remove returned false for present path")
	}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}

	db, _ = OpenDir(dir)
	if len(db.Dirs()) != 0 {
		t.Errorf("want empty db, got %d entries", len(db.Dirs()))
	}
	if db.Remove(path) {
		t.Error("Remove returned true for absent path")
	}
}

// TestAddVsAddUpdate: Add leaves last_accessed untouched on an existing entry;
// AddUpdate refreshes it (R-EDIT-2 vs R-ADD-1).
func TestAddVsAddUpdate(t *testing.T) {
	db, _ := OpenDir(t.TempDir())
	db.AddUpdate("/p", 1.0, 100, KindDir)

	db.Add("/p", 1.0, 200, KindDir) // rank +1, last_accessed unchanged
	d := db.Dirs()[0]
	if d.LastAccessed != 100 {
		t.Errorf("Add touched last_accessed: got %d, want 100", d.LastAccessed)
	}
	if math.Abs(d.Rank-2.0) > 1e-9 {
		t.Errorf("rank = %v, want 2.0", d.Rank)
	}

	db.AddUpdate("/p", 1.0, 300, KindDir) // rank +1, last_accessed -> 300
	d = db.Dirs()[0]
	if d.LastAccessed != 300 {
		t.Errorf("AddUpdate did not refresh last_accessed: got %d, want 300", d.LastAccessed)
	}
}

// TestContains reports presence without mutating rank or last_accessed.
func TestContains(t *testing.T) {
	db, _ := OpenDir(t.TempDir())
	if db.Contains("/p") {
		t.Error("Contains returned true for absent path")
	}
	db.AddUpdate("/p", 1.0, testEpoch, KindDir)
	if !db.Contains("/p") {
		t.Error("Contains returned false for present path")
	}
	if len(db.Dirs()) != 1 {
		t.Errorf("Contains mutated the database; got %d entries", len(db.Dirs()))
	}
}

// TestRankFloor: rank never goes negative on Add/AddUpdate (R-ADD-1, R-EDIT-2).
func TestRankFloor(t *testing.T) {
	db, _ := OpenDir(t.TempDir())
	db.AddUpdate("/p", 1.0, 100, KindDir)
	db.Add("/p", -5.0, 200, KindDir) // 1 - 5 = -4 -> floored to 0
	if r := db.Dirs()[0].Rank; r != 0.0 {
		t.Errorf("rank = %v, want 0.0 (floored)", r)
	}
}

// TestAge covers rescale + prune (A-4, R-DB-4): total>maxAge triggers scaling by
// 0.9*maxAge/total, dropping post-scale ranks below 1.0.
func TestAge(t *testing.T) {
	db, _ := OpenDir(t.TempDir())
	db.AddUpdate("/keep", 100.0, testEpoch, KindDir)
	db.AddUpdate("/drop", 2.0, testEpoch, KindDir)
	// total = 102 > 50; factor = 0.9*50/102 = 0.44117...
	// /keep -> 44.12 (kept); /drop -> 0.882 (<1, pruned)
	db.Age(50.0)

	if len(db.Dirs()) != 1 {
		t.Fatalf("want 1 entry after aging, got %d", len(db.Dirs()))
	}
	d := db.Dirs()[0]
	if d.Path != "/keep" {
		t.Errorf("kept wrong entry: %q", d.Path)
	}
	want := 100.0 * (0.9 * 50.0 / 102.0)
	if math.Abs(d.Rank-want) > 1e-9 {
		t.Errorf("scaled rank = %v, want %v", d.Rank, want)
	}
}

// TestAgeNoTrigger: total <= maxAge is a no-op.
func TestAgeNoTrigger(t *testing.T) {
	db, _ := OpenDir(t.TempDir())
	db.AddUpdate("/a", 5.0, testEpoch, KindDir)
	db.AddUpdate("/b", 5.0, testEpoch, KindDir)
	db.Age(10000.0)
	if len(db.Dirs()) != 2 {
		t.Errorf("aging should be a no-op below the ceiling; got %d entries", len(db.Dirs()))
	}
	if db.Dirs()[0].Rank != 5.0 {
		t.Errorf("ranks should be unchanged; got %v", db.Dirs()[0].Rank)
	}
}

// TestDedup merges same-path entries: sum ranks, max last_accessed (R-DB-5).
func TestDedup(t *testing.T) {
	db, _ := OpenDir(t.TempDir())
	db.AddUnchecked("/p", 1.0, 100)
	db.AddUnchecked("/p", 2.0, 300)
	db.AddUnchecked("/q", 5.0, 200)
	db.Dedup()

	if len(db.Dirs()) != 2 {
		t.Fatalf("want 2 entries after dedup, got %d", len(db.Dirs()))
	}
	var p *Dir
	for i := range db.Dirs() {
		if db.Dirs()[i].Path == "/p" {
			p = &db.Dirs()[i]
		}
	}
	if p == nil {
		t.Fatal("/p missing after dedup")
	}
	if math.Abs(p.Rank-3.0) > 1e-9 {
		t.Errorf("merged rank = %v, want 3.0", p.Rank)
	}
	if p.LastAccessed != 300 {
		t.Errorf("merged last_accessed = %d, want 300 (max)", p.LastAccessed)
	}
}

// TestSortDoesNotDirty enforces deviation D-4: sorting never marks the DB dirty,
// so a query that only reorders performs no rewrite.
func TestSortDoesNotDirty(t *testing.T) {
	db, _ := OpenDir(t.TempDir())
	db.AddUpdate("/a", 1.0, 100, KindDir)
	db.AddUpdate("/b", 2.0, 100, KindDir)
	db.Save() // clears dirty
	if db.Dirty() {
		t.Fatal("db should be clean after save")
	}
	db.SortByScore(1000)
	if db.Dirty() {
		t.Error("SortByScore marked db dirty (violates deviation D-4)")
	}
	db.SortByPath()
	if db.Dirty() {
		t.Error("SortByPath marked db dirty (violates deviation D-4)")
	}
}

// TestSaveNoOpWhenClean: Save is a no-op (no file written) when not dirty.
func TestSaveNoOpWhenClean(t *testing.T) {
	dir := t.TempDir()
	db, _ := OpenDir(dir)
	// Never mutated -> not dirty -> Save should not create the file (R-DB-3).
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}
	db2, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(db2.Dirs()) != 0 {
		t.Errorf("expected no file/empty db, got %d entries", len(db2.Dirs()))
	}
}
