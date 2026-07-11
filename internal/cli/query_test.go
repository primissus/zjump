package cli

import (
	"strings"
	"testing"
	"time"

	"zjump/internal/db"
)

func nowEpoch() db.Epoch { return db.Epoch(time.Now().Unix()) }

// preload builds a database under the current data dir via fn and saves it.
func preload(t *testing.T, fn func(*db.Database)) {
	t.Helper()
	database := openTestDB(t)
	fn(database)
	if err := database.Save(); err != nil {
		t.Fatal(err)
	}
}

// runQueryCapture runs `query args...` capturing stdout.
func runQueryCapture(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return captureStdout(t, func() error { return runQuery(args) })
}

// TestQueryTypeFilter: each --type value selects the right candidate kinds,
// `any` spans all DB kinds, and an unknown value is rejected (§5.1, R2-TYPE-1/2/3).
func TestQueryTypeFilter(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.AddUpdate("/plain/dir", 1.0, nowEpoch(), db.KindDir)
		d.AddUpdate("/some/repo", 1.0, nowEpoch(), db.KindRepo)
		d.PutAlias("myalias", "/alias/target", nowEpoch())
	})

	cases := []struct {
		typ         string
		wantContain []string
		wantAbsent  []string
	}{
		{"dir", []string{"/plain/dir"}, []string{"/some/repo", "/alias/target"}},
		{"repo", []string{"/some/repo"}, []string{"/plain/dir", "/alias/target"}},
		{"alias", []string{"/alias/target"}, []string{"/plain/dir", "/some/repo"}},
		{"any", []string{"/plain/dir", "/some/repo", "/alias/target"}, nil},
		{"", []string{"/plain/dir", "/some/repo"}, []string{"/alias/target"}}, // default: dir+repo
	}
	for _, c := range cases {
		args := []string{"-l", "--all"}
		if c.typ != "" {
			args = append(args, "--type", c.typ)
		}
		out, err := runQueryCapture(t, args...)
		if err != nil {
			t.Fatalf("type %q: %v", c.typ, err)
		}
		for _, want := range c.wantContain {
			if !strings.Contains(out, want) {
				t.Errorf("type %q output %q missing %q", c.typ, out, want)
			}
		}
		for _, absent := range c.wantAbsent {
			if strings.Contains(out, absent) {
				t.Errorf("type %q output %q should not contain %q", c.typ, out, absent)
			}
		}
	}

	// Invalid --type value is a hard error.
	if err := runQuery([]string{"--type", "bogus"}); err == nil || err.Error() != "invalid type: bogus" {
		t.Errorf("invalid type error = %v, want 'invalid type: bogus'", err)
	}
}

// TestAliasFastPathExactWins: an exact-name alias beats a longer prefix alias
// (§5.2 step 3, G-7).
func TestAliasFastPathExactWins(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("foo", "/exact/target", nowEpoch())
		d.PutAlias("foobar", "/prefix/target", nowEpoch())
	})
	out, err := runQueryCapture(t, "foo")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "/exact/target" {
		t.Errorf("output = %q, want /exact/target", out)
	}
}

// TestAliasFastPathPrefixBestScore: with no exact match, the highest-scoring
// prefix alias wins (§5.2 step 3).
func TestAliasFastPathPrefixBestScore(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("ab", "/high", nowEpoch()) // recent -> high score
		d.PutAlias("aa", "/low", 1)           // ancient -> low score
	})
	out, err := runQueryCapture(t, "a")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "/high" {
		t.Errorf("output = %q, want /high (best score)", out)
	}
}

// TestAliasFastPathPrefixTieLexicographic: equal scores break by lexicographically
// smaller name (§5.2 step 3).
func TestAliasFastPathPrefixTieLexicographic(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("ac", "/y", 1_000_000)
		d.PutAlias("ab", "/x", 1_000_000) // same score; smaller name wins
	})
	out, err := runQueryCapture(t, "a")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "/x" {
		t.Errorf("output = %q, want /x (lexicographic tiebreak on name 'ab')", out)
	}
}

// TestAliasFastPathExcludedFallsThrough: when the only alias candidate is
// dropped by --exclude, the query falls through to normal dir/repo matching
// (§5.2 step 5, G-8).
func TestAliasFastPathExcludedFallsThrough(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("foo", "/alias/target", nowEpoch())
		d.AddUpdate("/work/foo", 1.0, nowEpoch(), db.KindDir)
	})
	out, err := runQueryCapture(t, "--exclude", "/alias/target", "--all", "foo")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "/work/foo" {
		t.Errorf("output = %q, want /work/foo (fell through past excluded alias)", out)
	}
}

// TestAliasFastPathMultiKeywordSkipsFastPath: more than one keyword bypasses the
// fast path entirely (§5.2), so an alias is never consulted.
func TestAliasFastPathMultiKeywordSkipsFastPath(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("foo", "/alias/target", nowEpoch())
	})
	// Two keywords, no matching dir/repo -> "no match found" (proves the alias
	// was not used as a fast-path hit).
	_, err := runQueryCapture(t, "--all", "foo", "zzz")
	if err == nil || err.Error() != "no match found" {
		t.Errorf("err = %v, want 'no match found'", err)
	}
}

// TestAliasFastPathListModeSkipsFastPath: --list bypasses the fast path, so the
// alias is neither emitted nor rank-bumped (§5.2).
func TestAliasFastPathListModeSkipsFastPath(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("foo", "/alias/target", nowEpoch())
	})
	out, err := runQueryCapture(t, "-l", "--all", "foo")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "/alias/target") {
		t.Errorf("list mode leaked the alias target: %q", out)
	}
	// Rank must be unchanged (no TouchAlias in list mode).
	database := openTestDB(t)
	a := database.FindAlias("foo")
	if a == nil || a.Rank != 1.0 {
		t.Errorf("alias rank = %v, want 1.0 (untouched by list mode)", a)
	}
}

// TestAliasFastPathBumpsRankAndSaves: a fast-path hit bumps the alias rank by 1
// and persists it (§5.2 step 4, G-6).
func TestAliasFastPathBumpsRankAndSaves(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("foo", "/t", 1_000)
	})
	if _, err := runQueryCapture(t, "foo"); err != nil {
		t.Fatal(err)
	}
	// Reopen from disk: the bump must have been saved.
	database := openTestDB(t)
	a := database.FindAlias("foo")
	if a == nil {
		t.Fatal("alias vanished")
	}
	if a.Rank != 2.0 {
		t.Errorf("persisted rank = %v, want 2.0 (bumped +1)", a.Rank)
	}
	if a.LastAccessed == 1_000 {
		t.Error("last_accessed was not refreshed on the fast-path bump")
	}
}
