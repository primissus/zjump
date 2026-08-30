package db

import (
	"math"
	"testing"
)

// TestDedupMergesDirAndRepoToRepo: two same-path entries, one dir one repo, merge
// into a single repo entry with summed rank and max last_accessed (§3, R2-DB-1).
func TestDedupMergesDirAndRepoToRepo(t *testing.T) {
	database, _ := OpenDir(t.TempDir())
	database.dirs = []Dir{
		{Path: "/p", Rank: 1.0, LastAccessed: 100, Kind: KindDir},
		{Path: "/p", Rank: 2.0, LastAccessed: 300, Kind: KindRepo},
		{Path: "/q", Rank: 5.0, LastAccessed: 200, Kind: KindDir},
	}
	database.Dedup()

	if len(database.dirs) != 2 {
		t.Fatalf("want 2 entries after dedup, got %d", len(database.dirs))
	}
	var p *Dir
	for i := range database.dirs {
		if database.dirs[i].Path == "/p" {
			p = &database.dirs[i]
		}
	}
	if p == nil {
		t.Fatal("/p missing after dedup")
	}
	if p.Kind != KindRepo {
		t.Errorf("merged kind = %d, want KindRepo (upgrade wins)", p.Kind)
	}
	if math.Abs(p.Rank-3.0) > 1e-9 {
		t.Errorf("merged rank = %v, want 3.0", p.Rank)
	}
	if p.LastAccessed != 300 {
		t.Errorf("merged last_accessed = %d, want 300 (max)", p.LastAccessed)
	}
}

// TestDedupAliasByName: aliases dedup by name (not path). Same name merges (sum
// rank, max last_accessed, latest target wins); a same-target alias with a
// different name stays separate (§3). Kept for decode-faithfulness (A-5).
func TestDedupAliasByName(t *testing.T) {
	database, _ := OpenDir(t.TempDir())
	database.dirs = []Dir{
		{Path: "/old/target", Rank: 1.0, LastAccessed: 100, Kind: KindAlias, Name: "work"},
		{Path: "/new/target", Rank: 2.0, LastAccessed: 300, Kind: KindAlias, Name: "work"},
		{Path: "/new/target", Rank: 4.0, LastAccessed: 50, Kind: KindAlias, Name: "job"},
	}
	database.Dedup()

	if len(database.dirs) != 2 {
		t.Fatalf("want 2 aliases after dedup, got %d", len(database.dirs))
	}
	var work, job *Dir
	for i := range database.dirs {
		switch database.dirs[i].Name {
		case "work":
			work = &database.dirs[i]
		case "job":
			job = &database.dirs[i]
		}
	}
	if work == nil || job == nil {
		t.Fatalf("expected both 'work' and 'job' aliases to survive: %+v", database.dirs)
	}
	if math.Abs(work.Rank-3.0) > 1e-9 {
		t.Errorf("merged 'work' rank = %v, want 3.0", work.Rank)
	}
	if work.LastAccessed != 300 {
		t.Errorf("merged 'work' last_accessed = %d, want 300", work.LastAccessed)
	}
	if work.Path != "/new/target" {
		t.Errorf("merged 'work' target = %q, want /new/target (latest wins)", work.Path)
	}
	if job.Path != "/new/target" || math.Abs(job.Rank-4.0) > 1e-9 {
		t.Errorf("'job' should be untouched, got %+v", *job)
	}
}

// TestAgingSkipsAliasCull: an alias whose post-scale rank falls below 1.0 is kept
// (rescaled, not deleted), while a plain dir in the same situation is culled
// (§2, R2-DB-4).
func TestAgingSkipsAliasCull(t *testing.T) {
	database, _ := OpenDir(t.TempDir())
	database.dirs = []Dir{
		{Path: "/big", Rank: 100.0, LastAccessed: 0, Kind: KindDir},
		{Path: "/small", Rank: 2.0, LastAccessed: 0, Kind: KindDir},
		{Path: "/target", Rank: 2.0, LastAccessed: 0, Kind: KindAlias, Name: "a"},
	}
	// total = 104 > 50 -> factor = 0.9*50/104 = 0.4327. small/alias -> 0.865 (<1).
	factor := 0.9 * 50.0 / 104.0
	database.Age(50.0)

	var alias, small, big *Dir
	for i := range database.dirs {
		switch database.dirs[i].Path {
		case "/target":
			alias = &database.dirs[i]
		case "/small":
			small = &database.dirs[i]
		case "/big":
			big = &database.dirs[i]
		}
	}
	if small != nil {
		t.Errorf("/small should have been culled (rank %v < 1.0)", small.Rank)
	}
	if big == nil {
		t.Fatal("/big should have survived")
	}
	if alias == nil {
		t.Fatal("alias should survive the cull even below rank 1.0")
	}
	if want := 2.0 * factor; math.Abs(alias.Rank-want) > 1e-9 {
		t.Errorf("alias rank = %v, want rescaled %v", alias.Rank, want)
	}
	if alias.Rank >= 1.0 {
		t.Errorf("test precondition broken: alias rank %v should be < 1.0", alias.Rank)
	}
}

// TestLazyPruneSkipsAlias: the stream's existence/TTL lazy deletion removes a
// stale nonexistent dir but never an alias, even one whose target is missing and
// stale (§2, R2-DB-4).
func TestLazyPruneSkipsAlias(t *testing.T) {
	database, _ := OpenDir(t.TempDir())
	database.dirs = []Dir{
		{Path: "/nonexistent/dir/xyz", Rank: 5.0, LastAccessed: 1, Kind: KindDir},
		{Path: "/nonexistent/target/xyz", Rank: 5.0, LastAccessed: 1, Kind: KindAlias, Name: "a"},
	}
	now := Epoch(10 * MONTH) // far past the 3-month TTL for last_accessed = 1
	opts := NewStreamOptions(now).WithExists(true)
	stream := NewStream(database, opts)
	for stream.Next() != nil { // drain
	}

	var sawDir, sawAlias bool
	for i := range database.dirs {
		if database.dirs[i].Kind == KindAlias {
			sawAlias = true
		} else {
			sawDir = true
		}
	}
	if sawDir {
		t.Error("stale nonexistent dir should have been lazily pruned")
	}
	if !sawAlias {
		t.Error("alias must never be lazily pruned (deleted only by `alias rm`)")
	}
}

// TestKindUpgradeInMutators: Add/AddUpdate promote an existing dir entry to repo
// but never downgrade a repo back to dir (§2, R2-DB-4).
func TestKindUpgradeInMutators(t *testing.T) {
	database, _ := OpenDir(t.TempDir())
	database.AddUpdate("/p", 1.0, 100, KindDir)
	if database.dirs[0].Kind != KindDir {
		t.Fatalf("initial kind = %d, want KindDir", database.dirs[0].Kind)
	}
	database.AddUpdate("/p", 1.0, 200, KindRepo) // upgrade
	if database.dirs[0].Kind != KindRepo {
		t.Errorf("kind after repo add = %d, want KindRepo", database.dirs[0].Kind)
	}
	database.AddUpdate("/p", 1.0, 300, KindDir) // must not downgrade
	if database.dirs[0].Kind != KindRepo {
		t.Errorf("kind after dir add = %d, want KindRepo (never downgrade)", database.dirs[0].Kind)
	}
	database.Add("/p", 1.0, 400, KindDir) // Add path also preserves repo
	if database.dirs[0].Kind != KindRepo {
		t.Errorf("kind after Add = %d, want KindRepo", database.dirs[0].Kind)
	}
}
