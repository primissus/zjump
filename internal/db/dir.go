package db

import (
	"fmt"
	"strings"
)

// Rank is the raw frecency counter for a directory, independent of time decay.
type Rank = float64

// Epoch is a Unix timestamp in whole seconds.
type Epoch = uint64

// Kind tags each entry as a plain directory or a git repository root (PLAN-GIT.md
// §2, D-6). KindAlias is recognized by the decoder for forward compatibility but
// is never written on this port (A-1, A-5). Worktrees are never stored — they are
// derived live at query time — so there is deliberately no KindWorktree.
type Kind uint8

const (
	KindDir   Kind = 0 // plain directory (existing behavior; the default)
	KindRepo  Kind = 1 // root of a git repository (has .git)
	KindAlias Kind = 2 // user-named shortcut; recognized on decode, never written
)

// Time-bucket constants (seconds). MONTH is used only by the query TTL, never by
// score(). Mirrors zoxide's util.rs constants (ARCHITECTURE.md §4).
const (
	SECOND Epoch = 1
	MINUTE       = 60 * SECOND
	HOUR         = 60 * MINUTE
	DAY          = 24 * HOUR
	WEEK         = 7 * DAY
	MONTH        = 30 * DAY
)

// Dir is a single tracked directory: its path, raw rank, and last-access time.
//
// Unlike zoxide's zero-copy Cow<'a, str> (ARCHITECTURE.md §12), Go's GC and
// immutable strings make a plain owned string simplest; the self-referential
// ouroboros machinery is unnecessary here.
type Dir struct {
	Path         string
	Rank         Rank
	LastAccessed Epoch
	Kind         Kind
	Name         string // non-empty only for KindAlias (the alias name)
}

// IsAlias reports whether d is a user-named alias entry (decoded from a
// future-format file; never produced by this port — A-5).
func (d *Dir) IsAlias() bool { return d.Kind == KindAlias }

// Score computes the decayed frecency score: rank multiplied by a recency
// weight. Uses saturating subtraction so a future-dated last_accessed (clock
// skew) lands in the highest-weight bucket. Mirrors Dir::score (ARCHITECTURE.md
// §4, R-MATCH-4).
func (d *Dir) Score(now Epoch) Rank {
	var duration Epoch
	if now > d.LastAccessed {
		duration = now - d.LastAccessed
	} // else saturate to 0

	switch {
	case duration < HOUR:
		return d.Rank * 4.0
	case duration < DAY:
		return d.Rank * 2.0
	case duration < WEEK:
		return d.Rank * 0.5
	default:
		return d.Rank * 0.25
	}
}

// Display renders the path alone.
func (d *Dir) Display() string {
	return d.Path
}

// DisplayScore renders "{score:>6.1}{sep}{path}", clamping the score to
// [0.0, 9999.0] and formatting it in a fixed 6-char right-aligned field with one
// decimal place. Mirrors DirDisplay (ARCHITECTURE.md §4, R-MATCH-4).
func (d *Dir) DisplayScore(now Epoch, sep string) string {
	score := clamp(d.Score(now), 0.0, 9999.0)
	return fmt.Sprintf("%6.1f%s%s", score, sep, d.Path)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ClampScore clamps a decayed frecency score to [0, 9999] for display, the
// single shared helper (M3/L3) behind Dir.DisplayScore and the list/worktree
// printers.
func ClampScore(s Rank) Rank {
	return clamp(s, 0.0, 9999.0)
}

// toLower lowercases s. Mirrors util::to_lowercase (M4: the advertised ASCII
// fast path was identical on both branches, so it is just strings.ToLower).
func toLower(s string) string {
	return strings.ToLower(s)
}
