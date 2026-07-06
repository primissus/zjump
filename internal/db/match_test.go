package db

import "testing"

// TestMatchKeywords ports zoxide's keyword-matcher parity table verbatim
// (src/db/stream.rs) — case-folding, final-component anchoring, and
// overlap/order rejection (A-2, R-MATCH-1/2/3). Keywords are lowercased first,
// exactly as StreamOptions::with_keywords does before filter_by_keywords runs.
func TestMatchKeywords(t *testing.T) {
	cases := []struct {
		keywords []string
		path     string
		want     bool
	}{
		// Case normalization
		{[]string{"fOo", "bAr"}, "/foo/bar", true},
		// Last component
		{[]string{"ba"}, "/foo/bar", true},
		{[]string{"fo"}, "/foo/bar", false},
		// Slash as suffix
		{[]string{"foo/"}, "/foo", false},
		{[]string{"foo/"}, "/foo/bar", true},
		{[]string{"foo/"}, "/foo/bar/baz", false},
		{[]string{"foo", "/"}, "/foo", false},
		{[]string{"foo", "/"}, "/foo/bar", true},
		{[]string{"foo", "/"}, "/foo/bar/baz", true},
		// Split components
		{[]string{"/", "fo", "/", "ar"}, "/foo/bar", true},
		{[]string{"oo/ba"}, "/foo/bar", true},
		// Overlap
		{[]string{"foo", "o", "bar"}, "/foo/bar", false},
		{[]string{"/foo/", "/bar"}, "/foo/bar", false},
		{[]string{"/foo/", "/bar"}, "/foo/baz/bar", true},
	}

	for _, tc := range cases {
		lowered := make([]string, len(tc.keywords))
		for i, k := range tc.keywords {
			lowered[i] = toLower(k)
		}
		got := matchKeywords(lowered, tc.path)
		if got != tc.want {
			t.Errorf("matchKeywords(%q, %q) = %v, want %v", tc.keywords, tc.path, got, tc.want)
		}
	}
}

func TestMatchKeywordsEmpty(t *testing.T) {
	if !matchKeywords(nil, "/anything") {
		t.Error("no keywords should match every path")
	}
}
