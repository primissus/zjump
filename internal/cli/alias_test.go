package cli

import (
	"strings"
	"testing"

	"zjump/internal/db"
)

// TestAliasNameValidation tables the accept/reject rules from §5.4 (R2-AL-1).
func TestAliasNameValidation(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"", false},
		{".", false},
		{"..", false},
		{"-", false},
		{"~", false},
		{"-x", false},   // leading dash
		{"a/b", false},  // slash
		{"a b", false},  // space
		{"a\tb", false}, // tab
		{"a\nb", false}, // newline
		{"work", true},
		{"my-repo", true}, // dash not leading is fine
		{"proj.42", true},
		{"工作", true}, // multibyte
	}
	for _, c := range cases {
		err := validateAliasName(c.name)
		if c.ok && err != nil {
			t.Errorf("name %q: unexpected error %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("name %q: expected an 'invalid alias name' error", c.name)
		}
	}
}

// TestAliasReplaceKeepsRank: re-adding an existing name replaces the target in
// place while preserving rank and last_accessed (§5.4, R2-AL-1).
func TestAliasReplaceKeepsRank(t *testing.T) {
	setupDataDir(t)
	dirA := t.TempDir()
	dirB := t.TempDir()

	if err := runAlias([]string{"add", "foo", dirA}); err != nil {
		t.Fatal(err)
	}
	// Bump the alias so its rank is distinguishable from a fresh insert.
	database := openTestDB(t)
	database.TouchAlias("foo", 4242)
	if err := database.Save(); err != nil {
		t.Fatal(err)
	}

	if err := runAlias([]string{"add", "foo", dirB}); err != nil {
		t.Fatal(err)
	}
	database = openTestDB(t)
	a := database.FindAlias("foo")
	if a == nil {
		t.Fatal("alias vanished after re-add")
	}
	if a.Path != dirB {
		t.Errorf("target = %q, want %q (replaced in place)", a.Path, dirB)
	}
	if a.Rank != 2.0 || a.LastAccessed != 4242 {
		t.Errorf("re-add changed rank/last: rank=%v last=%d, want rank 2 last 4242 preserved", a.Rank, a.LastAccessed)
	}
}

// TestAliasRm: `alias rm` removes an existing alias and errors on a missing one
// (§5.4, R2-AL-1).
func TestAliasRm(t *testing.T) {
	setupDataDir(t)
	dir := t.TempDir()
	if err := runAlias([]string{"add", "foo", dir}); err != nil {
		t.Fatal(err)
	}
	if err := runAlias([]string{"rm", "foo"}); err != nil {
		t.Fatal(err)
	}
	if openTestDB(t).FindAlias("foo") != nil {
		t.Error("alias still present after rm")
	}
	err := runAlias([]string{"rm", "foo"})
	if err == nil || err.Error() != "alias not found: foo" {
		t.Errorf("rm of missing alias = %v, want 'alias not found: foo'", err)
	}
}

// TestAliasListOrderAndNoTargetDedup: list is ordered best decayed score first
// and shows every alias, including multiple aliases pointing at one target
// (§5.4, R2-AL-1).
func TestAliasListOrderAndNoTargetDedup(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("a", "/shared", 1)          // ancient -> low score
		d.PutAlias("b", "/shared", nowEpoch()) // recent  -> high score (same target as a)
		d.PutAlias("c", "/other", nowEpoch())  // recent  -> high score, ties with b
	})

	out, err := captureStdout(t, func() error { return runAlias([]string{"list"}) })
	if err != nil {
		t.Fatal(err)
	}
	lines := splitNonEmpty(out)
	if len(lines) != 3 {
		t.Fatalf("want 3 rows (no target dedup), got %d: %q", len(lines), out)
	}
	// Expected order: b, c (recent, tie broken by name), then a (ancient).
	wantNames := []string{"b", "c", "a"}
	for i, want := range wantNames {
		if !strings.HasPrefix(lines[i], want+"\t") {
			t.Errorf("row %d = %q, want it to start with %q", i, lines[i], want+"\t")
		}
	}
	// Both aliases to /shared must appear.
	if !strings.Contains(out, "a\t/shared") || !strings.Contains(out, "b\t/shared") {
		t.Errorf("expected both /shared aliases in output: %q", out)
	}
}

// TestAliasListScoreColumn: --score prepends a decayed-score column (§5.4).
func TestAliasListScoreColumn(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("a", "/x", nowEpoch())
	})
	out, err := captureStdout(t, func() error { return runAlias([]string{"list", "--score"}) })
	if err != nil {
		t.Fatal(err)
	}
	// "<score>\ta\t/x" — three tab-separated fields.
	fields := strings.Split(strings.TrimSpace(out), "\t")
	if len(fields) != 3 || fields[1] != "a" || fields[2] != "/x" {
		t.Errorf("scored row = %q, want '<score>\\ta\\t/x'", out)
	}
}

// TestRemoveCannotDeleteAlias: `zjump remove` never deletes an alias, even when
// the removed path equals the alias target (§3, R2-AL-2).
func TestRemoveCannotDeleteAlias(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.PutAlias("foo", "/t", nowEpoch())
	})
	err := runRemove([]string{"/t"})
	if err == nil || err.Error() != "path not found in database: /t" {
		t.Errorf("remove of an alias target = %v, want 'path not found in database: /t'", err)
	}
	if openTestDB(t).FindAlias("foo") == nil {
		t.Error("remove deleted the alias")
	}
}

// splitNonEmpty splits s into non-empty lines.
func splitNonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
