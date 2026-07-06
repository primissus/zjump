// Package glob implements the subset of the Rust `glob` crate's Pattern matching
// that zoxide relies on for _ZO_EXCLUDE_DIRS (config.rs, db/stream.rs). zoxide
// calls Pattern::matches with default MatchOptions (require_literal_separator =
// false), so `*` and `?` DO match path separators — i.e. a lone `*` behaves like
// regex `.*` and `?` like `.`. A `**` that forms a whole path component is the
// distinct AnyRecursiveSequence: it matches zero or more components and collapses
// a surrounding separator, so `a/**/b` matches `a/b` (which `a/*/b` does not).
// The glob crate has NO backslash escaping — `\` is an ordinary literal (which is
// why Escape bracket-wraps metacharacters). Matching is whole-string (anchored)
// and case-sensitive.
package glob

import (
	"fmt"
	"regexp"
	"strings"
)

// Glob is a compiled exclude pattern.
type Glob struct {
	re  *regexp.Regexp
	src string
}

// New compiles a glob pattern. Supports `*`/`**` (any run, incl. separators),
// `?` (any single char), `[...]`/`[!...]` character classes, and `\` escaping.
func New(pattern string) (*Glob, error) {
	expr, err := translate(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob %q: %w", pattern, err)
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid glob %q: %w", pattern, err)
	}
	return &Glob{re: re, src: pattern}, nil
}

// Match reports whether the whole path matches the pattern.
func (g *Glob) Match(path string) bool { return g.re.MatchString(path) }

// String returns the original pattern text.
func (g *Glob) String() string { return g.src }

// Escape returns a pattern that matches s literally, wrapping each glob
// metacharacter in a single-character class. Mirrors glob::Pattern::escape,
// used for the default home-directory exclude (R-ENV-3).
func Escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '?', '*', '[', ']':
			b.WriteByte('[')
			b.WriteRune(r)
			b.WriteByte(']')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// translate converts a glob pattern into an anchored, dot-matches-all regexp.
func translate(pattern string) (string, error) {
	var b strings.Builder
	b.WriteString("(?s)^")

	i, n := 0, len(pattern)
	for i < n {
		c := pattern[i]
		switch c {
		case '/':
			// A component-boundary "/**" or "/**/" globstar collapses its
			// surrounding separators (matches zero or more components).
			if isGlobstar(pattern, i+1) {
				if i+3 < n && pattern[i+3] == '/' {
					b.WriteString("/(?:.*/)?") // "/**/"
					i += 4
				} else { // "/**" at end
					b.WriteString("(?:/.*)?")
					i += 3
				}
				continue
			}
			b.WriteByte('/')
			i++
		case '*':
			// A leading "**" or "**/" globstar (component-initial at start).
			if i == 0 && isGlobstar(pattern, 0) {
				if n > 2 && pattern[2] == '/' {
					b.WriteString("(?:.*/)?") // "**/"
					i = 3
					continue
				}
				if n == 2 { // "**" alone
					b.WriteString(".*")
					i = 2
					continue
				}
				// "**x" (x != '/'): not a globstar — fall through to plain '*'.
			}
			// A lone '*' (or a non-component '**') matches any run, including
			// separators (require_literal_separator = false).
			for i < n && pattern[i] == '*' {
				i++
			}
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
			i++
		case '[':
			j := i + 1
			neg := false
			if j < n && pattern[j] == '!' {
				neg = true
				j++
			}
			var class strings.Builder
			// A ']' immediately after '[' or '[!' is a literal ']'.
			if j < n && pattern[j] == ']' {
				class.WriteString(`\]`)
				j++
			}
			for j < n && pattern[j] != ']' {
				ch := pattern[j]
				// No backslash escaping: '\' is an ordinary class member.
				switch ch {
				case '^', '\\', ']':
					class.WriteByte('\\')
					class.WriteByte(ch)
				default:
					class.WriteByte(ch) // '-' passes through so ranges work
				}
				j++
			}
			if j >= n {
				return "", fmt.Errorf("unterminated character class")
			}
			b.WriteByte('[')
			if neg {
				b.WriteByte('^')
			}
			b.WriteString(class.String())
			b.WriteByte(']')
			i = j + 1
		default:
			// Everything else — including a literal '\' — is quoted literally.
			b.WriteString(regexp.QuoteMeta(string(c)))
			i++
		}
	}

	b.WriteString("$")
	return b.String(), nil
}

// isGlobstar reports whether an exactly-two-star run at idx forms a full path
// component (bounded on the right by '/' or end-of-pattern). The caller
// guarantees the left boundary (pattern start or a preceding '/'). A run of one
// or three-plus stars is not a globstar.
func isGlobstar(p string, idx int) bool {
	return idx+1 < len(p) && p[idx] == '*' && p[idx+1] == '*' &&
		(idx+2 == len(p) || p[idx+2] == '/')
}
