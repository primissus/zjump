package db

import (
	"os"

	"zjump/internal/glob"
)

// StreamOptions configures a candidate Stream. Build it with NewStreamOptions
// and the With* setters (mirrors zoxide's StreamOptions builder).
type StreamOptions struct {
	now             Epoch
	keywords        []string // lowercased
	exclude         []*glob.Glob
	exists          bool
	resolveSymlinks bool
	ttl             Epoch
	baseDir         *string
}

// NewStreamOptions returns options with the lazy-deletion TTL defaulted to
// now − 3×MONTH (90 days), matching zoxide (ARCHITECTURE.md §5, R-QRY-10b).
func NewStreamOptions(now Epoch) StreamOptions {
	var ttl Epoch
	if now > 3*MONTH {
		ttl = now - 3*MONTH
	} // else saturate to 0
	return StreamOptions{now: now, ttl: ttl}
}

// WithKeywords sets the match keywords (lowercased here, as zoxide does).
func (o StreamOptions) WithKeywords(keywords []string) StreamOptions {
	o.keywords = make([]string, len(keywords))
	for i, k := range keywords {
		o.keywords[i] = toLower(k)
	}
	return o
}

// WithExclude sets the _ZJUMP_EXCLUDE_DIRS globs (lazily purged on match).
func (o StreamOptions) WithExclude(exclude []*glob.Glob) StreamOptions {
	o.exclude = exclude
	return o
}

// WithExists enables the filesystem-existence filter (and its stale-entry lazy
// deletion). Disabled by --all (R-QRY-6).
func (o StreamOptions) WithExists(exists bool) StreamOptions {
	o.exists = exists
	return o
}

// WithResolveSymlinks toggles symlink resolution in the existence check.
func (o StreamOptions) WithResolveSymlinks(v bool) StreamOptions {
	o.resolveSymlinks = v
	return o
}

// WithBaseDir restricts results to entries component-wise under baseDir (used
// verbatim, never resolved — R-QRY-8). A nil pointer means no restriction.
func (o StreamOptions) WithBaseDir(baseDir *string) StreamOptions {
	o.baseDir = baseDir
	return o
}

// Stream yields matching directories best-first. It sorts the database by score
// on construction, then walks indices in reverse. Non-matches are skipped;
// excluded and stale-nonexistent entries are lazily removed from the DB as a
// side effect of iterating past them (ARCHITECTURE.md §5).
type Stream struct {
	db   *Database
	idx  int
	opts StreamOptions
}

// NewStream sorts db ascending by score and returns a best-first Stream.
func NewStream(db *Database, opts StreamOptions) *Stream {
	db.SortByScore(opts.now)
	return &Stream{db: db, idx: len(db.dirs) - 1, opts: opts}
}

// Next returns the next best-matching directory, or nil when exhausted. The
// filter order is exact: keywords, base-dir, exclude (lazy delete), exists
// (lazy delete when stale). Mirrors Stream::next.
func (s *Stream) Next() *Dir {
	for s.idx >= 0 {
		idx := s.idx
		s.idx--
		// Invariant (see design note): idx < len always holds even as
		// swap_remove shrinks the slice; the guard is defensive only.
		if idx >= len(s.db.dirs) {
			continue
		}
		dir := &s.db.dirs[idx]

		if !matchKeywords(s.opts.keywords, dir.Path) {
			continue
		}
		if !s.filterByBaseDir(dir.Path) {
			continue
		}
		if !s.filterByExclude(dir.Path) {
			// Aliases are exempt from lazy deletion — deleted only by
			// `alias rm` (§2, R2-DB-4) — so skip without removing.
			if !dir.IsAlias() {
				s.db.swapRemove(idx)
			}
			continue
		}
		// Existence is the slowest check, so it goes last.
		if !s.filterByExists(dir.Path) {
			if dir.LastAccessed < s.opts.ttl && !dir.IsAlias() {
				s.db.swapRemove(idx)
			}
			continue
		}
		return &s.db.dirs[idx]
	}
	return nil
}

func (s *Stream) filterByBaseDir(path string) bool {
	if s.opts.baseDir == nil {
		return true
	}
	return pathHasPrefix(path, *s.opts.baseDir)
}

func (s *Stream) filterByExclude(path string) bool {
	for _, g := range s.opts.exclude {
		if g.Match(path) {
			return false
		}
	}
	return true
}

func (s *Stream) filterByExists(path string) bool {
	if !s.opts.exists {
		return true
	}
	// Inverted deliberately: if symlinks were resolved at add-time, don't
	// re-resolve at query-time (ARCHITECTURE.md §5).
	var info os.FileInfo
	var err error
	if s.opts.resolveSymlinks {
		info, err = os.Lstat(path)
	} else {
		info, err = os.Stat(path)
	}
	if err != nil {
		return false
	}
	return info.IsDir()
}

// pathHasPrefix reports whether path is component-wise under base (so "/foo"
// does not match "/foobar"). Equivalent to Rust's Path::starts_with.
func pathHasPrefix(path, base string) bool {
	pc := splitComponents(path)
	bc := splitComponents(base)
	if len(bc) > len(pc) {
		return false
	}
	for i := range bc {
		if pc[i] != bc[i] {
			return false
		}
	}
	return true
}

// splitComponents splits a path into components the way Path::components does
// for prefix comparison: a leading "/" is its own root component, and empty /
// "." segments are dropped.
func splitComponents(p string) []string {
	var comps []string
	if len(p) > 0 && p[0] == '/' {
		comps = append(comps, "/")
	}
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			seg := p[start:i]
			if seg != "" && seg != "." {
				comps = append(comps, seg)
			}
			start = i + 1
		}
	}
	return comps
}
