package glob_test

import (
	"testing"

	"zjump/internal/glob"
)

func mustNew(t *testing.T, pattern string) *glob.Glob {
	t.Helper()
	g, err := glob.New(pattern)
	if err != nil {
		t.Fatalf("New(%q): %v", pattern, err)
	}
	return g
}

// Under zoxide's default MatchOptions, `*` matches across path separators.
func TestStarCrossesSeparator(t *testing.T) {
	g := mustNew(t, "/home*")
	for _, s := range []string{"/home", "/home/law", "/home/law/deep"} {
		if !g.Match(s) {
			t.Errorf("%q should match /home*", s)
		}
	}
	if g.Match("/other") {
		t.Error("/other should not match /home*")
	}
}

func TestQuestion(t *testing.T) {
	g := mustNew(t, "/a?c")
	if !g.Match("/abc") {
		t.Error("/abc should match /a?c")
	}
	if g.Match("/ac") {
		t.Error("/ac should not match /a?c")
	}
}

func TestCharClass(t *testing.T) {
	g := mustNew(t, "/[abc]x")
	if !g.Match("/ax") || !g.Match("/bx") {
		t.Error("/ax and /bx should match /[abc]x")
	}
	if g.Match("/dx") {
		t.Error("/dx should not match /[abc]x")
	}
}

func TestNegatedClass(t *testing.T) {
	g := mustNew(t, "/[!a]x")
	if !g.Match("/bx") {
		t.Error("/bx should match /[!a]x")
	}
	if g.Match("/ax") {
		t.Error("/ax should not match /[!a]x")
	}
}

func TestDoubleStar(t *testing.T) {
	g := mustNew(t, "**/node_modules")
	if !g.Match("/a/b/node_modules") {
		t.Error("/a/b/node_modules should match **/node_modules")
	}
}

// Globstar (**) is AnyRecursiveSequence: it matches zero or more path
// components, collapsing a surrounding separator — the distinction from a lone
// '*'. Verified against the real Rust glob crate during the parity audit.
func TestGlobstarZeroComponents(t *testing.T) {
	mid := mustNew(t, "/home/user/**/node_modules")
	for _, s := range []string{
		"/home/user/node_modules",     // zero intermediate components
		"/home/user/a/node_modules",   // one
		"/home/user/a/b/node_modules", // two
	} {
		if !mid.Match(s) {
			t.Errorf("%q should match /home/user/**/node_modules", s)
		}
	}

	lead := mustNew(t, "**/target")
	for _, s := range []string{"target", "x/target", "x/y/target"} {
		if !lead.Match(s) {
			t.Errorf("%q should match **/target", s)
		}
	}

	trail := mustNew(t, "/var/log/**")
	for _, s := range []string{"/var/log", "/var/log/a", "/var/log/a/b"} {
		if !trail.Match(s) {
			t.Errorf("%q should match /var/log/**", s)
		}
	}

	// A single '*' does NOT collapse the separator: /a/*/b must not match /a/b.
	star := mustNew(t, "/a/*/b")
	if star.Match("/a/b") {
		t.Error("/a/b must NOT match /a/*/b (single star does not collapse '/')")
	}
	if !star.Match("/a/x/b") {
		t.Error("/a/x/b should match /a/*/b")
	}
}

// The glob crate has no backslash escaping: '\' is a literal character, so a
// pattern is the exact inverse of treating '\' as an escape.
func TestBackslashIsLiteral(t *testing.T) {
	g := mustNew(t, `/a\*`) // '/', 'a', literal '\', '*' (AnySequence)
	if !g.Match(`/a\foo`) {
		t.Error(`/a\foo should match /a\*  ('\' is literal, '*' is wildcard)`)
	}
	if g.Match("/astar") {
		t.Error(`/astar must NOT match /a\*  (no backslash present)`)
	}
}

// Escape produces a pattern matching the input literally and non-recursively —
// the behavior the default home-directory exclude relies on (R-ENV-3).
func TestEscapeLiteral(t *testing.T) {
	home := "/home/law"
	g := mustNew(t, glob.Escape(home))
	if !g.Match(home) {
		t.Errorf("escaped home should match itself")
	}
	if g.Match("/home/law/sub") {
		t.Error("escaped home must NOT match subdirectories (literal only)")
	}
}

func TestEscapeSpecialChars(t *testing.T) {
	for _, lit := range []string{"/a[b]c", "/weird]dir", "/star*here", "/q?mark", "/a[!x]b"} {
		g := mustNew(t, glob.Escape(lit))
		if !g.Match(lit) {
			t.Errorf("escaped %q should match itself", lit)
		}
	}
	// A glob-escaped literal must not behave as a wildcard.
	g := mustNew(t, glob.Escape("/star*here"))
	if g.Match("/starXXXhere") {
		t.Error("escaped '*' must be literal, not a wildcard")
	}
}

func TestInvalidGlob(t *testing.T) {
	if _, err := glob.New("/a[bc"); err == nil {
		t.Error("expected error on unterminated character class")
	}
}
