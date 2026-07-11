package db

import "strings"

// matchKeywords reports whether path matches the given keywords using zoxide's
// exact right-to-left substring algorithm (ARCHITECTURE.md §5, R-MATCH-1/2/3).
//
// keywords MUST already be lowercased (as StreamOptions does); the path is
// lowercased here. All indexing is byte-wise, consistent between the lowered
// path and lowered keywords, exactly as the Rust `str::rfind` / `.len()` pair.
//
//   - No keywords => every path matches.
//   - The LAST keyword must occur (rightmost) within the final path component:
//     nothing after its match may contain a path separator.
//   - Each earlier keyword (processed right-to-left) must then be found strictly
//     to the left of the previously matched span, in order — spans cannot
//     overlap and must preserve left-to-right order.
func matchKeywords(keywords []string, path string) bool {
	if len(keywords) == 0 {
		return true
	}
	last := keywords[len(keywords)-1]
	rest := keywords[:len(keywords)-1]

	p := toLower(path)

	idx := strings.LastIndex(p, last)
	if idx < 0 {
		return false
	}
	// The last keyword must end within the final path component.
	if containsSeparator(p[idx+len(last):]) {
		return false
	}
	p = p[:idx]

	// Earlier keywords, right-to-left, each strictly left of the last span.
	for i := len(rest) - 1; i >= 0; i-- {
		j := strings.LastIndex(p, rest[i])
		if j < 0 {
			return false
		}
		p = p[:j]
	}
	return true
}

// MatchPath reports whether target matches keywords with the standard
// right-to-left substring matcher, lowercasing the keywords first exactly as the
// stream does. Exposed for the worktree pipeline, which matches keywords against
// worktree paths outside the DB stream (§6, R2-WT-2).
func MatchPath(keywords []string, target string) bool {
	lowered := make([]string, len(keywords))
	for i, k := range keywords {
		lowered[i] = toLower(k)
	}
	return matchKeywords(lowered, target)
}

// containsSeparator reports whether s contains a path separator. Unix-only
// (N-4): the separator is '/'. Mirrors path::is_separator on Unix.
func containsSeparator(s string) bool {
	return strings.IndexByte(s, '/') >= 0
}
