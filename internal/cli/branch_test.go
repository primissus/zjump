package cli

import (
	"strings"
	"testing"
)

// TestBranchMoveToFrontAndMarkers: the current branch is moved to the front and
// marked "*", the rest keep their order and get a " " marker (§5.5 steps 3, 5).
func TestBranchMoveToFrontAndMarkers(t *testing.T) {
	// git for-each-ref order (most-recent first); "main" is current.
	branches := []string{"feature", "main", "bugfix"}
	ordered := moveToFront(branches, "main")
	if want := []string{"main", "feature", "bugfix"}; !equalStrings(ordered, want) {
		t.Fatalf("moveToFront = %v, want %v", ordered, want)
	}

	records := branchDisplayRecords(ordered, "main")
	if records[0] != "*\tmain" {
		t.Errorf("record[0] = %q, want '*\\tmain' (current marked and first)", records[0])
	}
	for _, r := range records[1:] {
		if !strings.HasPrefix(r, " \t") {
			t.Errorf("non-current record %q should start with ' \\t'", r)
		}
	}
}

// TestBranchMoveToFrontAbsent: a current value not in the list leaves order
// unchanged (detached HEAD reports "" and is never marked).
func TestBranchMoveToFrontAbsent(t *testing.T) {
	branches := []string{"a", "b"}
	if got := moveToFront(branches, ""); !equalStrings(got, branches) {
		t.Errorf("moveToFront with absent target = %v, want unchanged %v", got, branches)
	}
}

// TestBranchSingleSubstringMatch covers the fast-path selector (§5.5 step 4):
// exactly one case-sensitive substring match wins; zero or many falls through.
func TestBranchSingleSubstringMatch(t *testing.T) {
	branches := []string{"main", "feature", "feature-2", "bugfix"}
	cases := []struct {
		pattern string
		want    string
		ok      bool
	}{
		{"bug", "bugfix", true},    // unique
		{"feature", "", false},     // matches two
		{"nope", "", false},        // matches none
		{"Main", "", false},        // case-sensitive: no match
		{"main", "main", true},     // unique exact-substring
		{"bugfix", "bugfix", true}, // full name
	}
	for _, c := range cases {
		got, ok := singleSubstringMatch(branches, c.pattern)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("pattern %q: got (%q,%v), want (%q,%v)", c.pattern, got, ok, c.want, c.ok)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
