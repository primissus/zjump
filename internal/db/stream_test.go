package db

import (
	"path/filepath"
	"testing"

	"zjump/internal/glob"
)

func collect(s *Stream) []string {
	var out []string
	for {
		d := s.Next()
		if d == nil {
			return out
		}
		out = append(out, d.Path)
	}
}

// TestStreamOrdering: highest decayed score first (R-MATCH-4).
func TestStreamOrdering(t *testing.T) {
	const now Epoch = 1000
	db := &Database{dirs: []Dir{
		{Path: "/a", Rank: 1, LastAccessed: now},  // score 4
		{Path: "/b", Rank: 10, LastAccessed: now}, // score 40
		{Path: "/c", Rank: 5, LastAccessed: now},  // score 20
	}}
	got := collect(NewStream(db, NewStreamOptions(now)))
	want := []string{"/b", "/c", "/a"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestStreamExcludeLazyDelete: an _ZJUMP_EXCLUDE_DIRS glob match permanently
// removes the entry from the DB the moment it's evaluated (R-QRY-10a).
func TestStreamExcludeLazyDelete(t *testing.T) {
	const now Epoch = 1000
	g, err := glob.New("/secret*")
	if err != nil {
		t.Fatal(err)
	}
	db := &Database{dirs: []Dir{
		{Path: "/secret/x", Rank: 1, LastAccessed: now},
		{Path: "/ok", Rank: 1, LastAccessed: now},
	}}
	opts := NewStreamOptions(now).WithExclude([]*glob.Glob{g})
	got := collect(NewStream(db, opts))
	if len(got) != 1 || got[0] != "/ok" {
		t.Fatalf("results = %v, want [/ok]", got)
	}
	if len(db.dirs) != 1 || db.dirs[0].Path != "/ok" {
		t.Errorf("excluded entry not deleted; db = %+v", db.dirs)
	}
	if !db.dirty {
		t.Error("lazy deletion should mark db dirty")
	}
}

// TestStreamExists: existing dirs are returned; missing+stale entries are
// deleted; missing+fresh entries are hidden but retained (R-QRY-10b).
func TestStreamExists(t *testing.T) {
	existing := t.TempDir()
	missingStale := filepath.Join(existing, "gone-stale")
	missingFresh := filepath.Join(existing, "gone-fresh")

	const now = 10 * MONTH // ttl = now - 3*MONTH
	const stale Epoch = 0  // < ttl -> delete
	fresh := now           // >= ttl -> retain

	db := &Database{dirs: []Dir{
		{Path: existing, Rank: 3, LastAccessed: now},
		{Path: missingStale, Rank: 2, LastAccessed: stale},
		{Path: missingFresh, Rank: 1, LastAccessed: fresh},
	}}
	opts := NewStreamOptions(now).WithExists(true)
	got := collect(NewStream(db, opts))
	if len(got) != 1 || got[0] != existing {
		t.Fatalf("results = %v, want [%s]", got, existing)
	}

	// Stale-missing deleted; existing + fresh-missing retained.
	if len(db.dirs) != 2 {
		t.Fatalf("db should retain 2 entries, has %d: %+v", len(db.dirs), db.dirs)
	}
	for _, d := range db.dirs {
		if d.Path == missingStale {
			t.Errorf("stale-missing entry should have been deleted")
		}
	}
}

// TestStreamAllDisablesExistence: with exists off (i.e. --all), nonexistent dirs
// are returned and none are deleted (R-QRY-6).
func TestStreamAllDisablesExistence(t *testing.T) {
	const now = 10 * MONTH
	db := &Database{dirs: []Dir{
		{Path: "/does/not/exist", Rank: 1, LastAccessed: 0},
	}}
	opts := NewStreamOptions(now) // exists defaults to false
	got := collect(NewStream(db, opts))
	if len(got) != 1 {
		t.Fatalf("results = %v, want the nonexistent entry included", got)
	}
	if len(db.dirs) != 1 {
		t.Error("nothing should be deleted when the existence filter is off")
	}
}

// TestStreamBaseDir: component-wise subtree restriction, used verbatim (R-QRY-8).
func TestStreamBaseDir(t *testing.T) {
	const now Epoch = 1000
	db := &Database{dirs: []Dir{
		{Path: "/home/law/proj", Rank: 1, LastAccessed: now},
		{Path: "/home/law2", Rank: 1, LastAccessed: now}, // must NOT match /home/law
		{Path: "/tmp/x", Rank: 1, LastAccessed: now},
	}}
	base := "/home/law"
	opts := NewStreamOptions(now).WithBaseDir(&base)
	got := collect(NewStream(db, opts))
	if len(got) != 1 || got[0] != "/home/law/proj" {
		t.Fatalf("results = %v, want [/home/law/proj]", got)
	}
}

// TestStreamKeywordFilter: end-to-end keyword filtering through the stream.
func TestStreamKeywordFilter(t *testing.T) {
	const now Epoch = 1000
	db := &Database{dirs: []Dir{
		{Path: "/foo/bar", Rank: 1, LastAccessed: now},
		{Path: "/baz/qux", Rank: 5, LastAccessed: now},
	}}
	opts := NewStreamOptions(now).WithKeywords([]string{"BAR"})
	got := collect(NewStream(db, opts))
	if len(got) != 1 || got[0] != "/foo/bar" {
		t.Fatalf("results = %v, want [/foo/bar]", got)
	}
}
