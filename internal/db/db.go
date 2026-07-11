// Package db implements zjump's frecency database: the on-disk versioned binary
// format, atomic persistence, the rank mutators, the aging/dedup passes, and the
// candidate match stream. It mirrors zoxide's engine (ARCHITECTURE.md §3–5)
// against a zjump-native format (D-1).
package db

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Filename is the database file's name within the data directory. Together with
// the zjump-specific data directory it can never collide with zoxide's
// "zoxide/db.zo" (R-DB-1, D-1).
const Filename = "db"

// Database is an in-memory view of the on-disk file plus a dirty flag. All
// ranking, matching, aging, and dedup operate on the in-memory slice; it is
// atomically rewritten by Save only when actually dirty.
type Database struct {
	path  string
	dirs  []Dir
	dirty bool
}

// OpenDir opens (or lazily initializes) the database under dataDir. The data
// directory is created eagerly; the file itself is not written until the first
// Save with dirty data (R-DB-3). Mirrors Database::open_dir.
func OpenDir(dataDir string) (*Database, error) {
	path := filepath.Join(dataDir, Filename)
	if resolved, err := filepath.Abs(path); err == nil {
		path = resolved
	}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		dirs, derr := deserialize(data)
		if derr != nil {
			return nil, derr
		}
		return &Database{path: path, dirs: dirs}, nil
	case errors.Is(err, fs.ErrNotExist):
		// Create the data directory, but not the file yet.
		if mkErr := os.MkdirAll(dataDir, 0o700); mkErr != nil {
			return nil, fmt.Errorf("unable to create data directory: %s: %w", dataDir, mkErr)
		}
		return &Database{path: path, dirs: nil}, nil
	default:
		return nil, fmt.Errorf("could not read from database: %s: %w", path, err)
	}
}

// Dirs returns the current in-memory entries.
func (db *Database) Dirs() []Dir { return db.dirs }

// Dirty reports whether the database has unsaved changes.
func (db *Database) Dirty() bool { return db.dirty }

// Save atomically rewrites the file, but only when dirty (a no-op otherwise —
// R-DB-2, D-4). Mirrors Database::save.
func (db *Database) Save() error {
	if !db.dirty {
		return nil
	}
	data, err := serialize(db.dirs)
	if err != nil {
		return fmt.Errorf("could not serialize database: %w", err)
	}
	if err := writeAtomic(db.path, data); err != nil {
		return fmt.Errorf("could not write to database: %w", err)
	}
	db.dirty = false
	return nil
}

// Add increments the rank of an existing path entry (floored at 0), leaving
// last_accessed untouched; on a missing path it inserts a fresh entry with
// last_accessed = now. The kind applies the dir->repo upgrade rule (§2): an
// existing entry is promoted to KindRepo if either it or kind is KindRepo, and
// is never downgraded. Used by `edit increment/decrement` (R-EDIT-2/4). Mirrors
// Database::add.
func (db *Database) Add(path string, by Rank, now Epoch, kind Kind) {
	if d := db.findPath(path); d != nil {
		d.Rank = max0(d.Rank + by)
		d.Kind = upgradeKind(d.Kind, kind)
	} else {
		db.dirs = append(db.dirs, Dir{Path: path, Rank: max0(by), LastAccessed: now, Kind: kind})
	}
	db.dirty = true
}

// AddUpdate increments rank (floored at 0) AND sets last_accessed = now on an
// existing path entry; inserts otherwise. Applies the same dir->repo upgrade
// rule as Add (§2). Used by `add` (R-ADD-1). Mirrors Database::add_update.
func (db *Database) AddUpdate(path string, by Rank, now Epoch, kind Kind) {
	if d := db.findPath(path); d != nil {
		d.Rank = max0(d.Rank + by)
		d.LastAccessed = now
		d.Kind = upgradeKind(d.Kind, kind)
	} else {
		db.dirs = append(db.dirs, Dir{Path: path, Rank: max0(by), LastAccessed: now, Kind: kind})
	}
	db.dirty = true
}

// PutAlias creates or replaces an alias (G-6, §3, §5.4, D-6). A new name inserts
// with rank 1.0 and last_accessed = now; an existing name replaces its target
// path in place, preserving rank and last_accessed. Alias identity is by name.
func (db *Database) PutAlias(name, path string, now Epoch) {
	if d := db.findAlias(name); d != nil {
		d.Path = path
	} else {
		db.dirs = append(db.dirs, Dir{Path: path, Rank: 1.0, LastAccessed: now, Kind: KindAlias, Name: name})
	}
	db.dirty = true
}

// TouchAlias bumps an alias on use (G-6): rank += 1.0 and last_accessed = now.
// Returns whether an alias with that name existed. Mirrors the alias fast-path
// mutation (§5.2 step 4, D-6).
func (db *Database) TouchAlias(name string, now Epoch) bool {
	d := db.findAlias(name)
	if d == nil {
		return false
	}
	d.Rank += 1.0
	d.LastAccessed = now
	db.dirty = true
	return true
}

// RemoveAlias deletes the alias with the given name, returning whether one was
// found (§5.4 `alias rm`). Aliases are deleted only through this path (§2).
func (db *Database) RemoveAlias(name string) bool {
	for i := range db.dirs {
		if db.dirs[i].Kind == KindAlias && db.dirs[i].Name == name {
			db.swapRemove(i)
			return true
		}
	}
	return false
}

// FindAlias returns the alias entry with the given name, or nil.
func (db *Database) FindAlias(name string) *Dir { return db.findAlias(name) }

// AddUnchecked pushes a new entry unconditionally (duplicates expected, no rank
// floor). Only reachable via `import`, which is out of scope (N-1); retained for
// future work F-3 and exercised only by tests. Mirrors Database::add_unchecked.
func (db *Database) AddUnchecked(path string, rank Rank, now Epoch) {
	db.dirs = append(db.dirs, Dir{Path: path, Rank: rank, LastAccessed: now})
	db.dirty = true
}

// Remove deletes the dir/repo entry whose path is an exact string match,
// returning whether one was found. Aliases are never matched — they are deleted
// only through RemoveAlias (§3). O(1), order-disturbing (swap-remove). Mirrors
// Database::remove.
func (db *Database) Remove(path string) bool {
	for i := range db.dirs {
		if db.dirs[i].Kind != KindAlias && db.dirs[i].Path == path {
			db.swapRemove(i)
			return true
		}
	}
	return false
}

// swapRemove removes index i by moving the last element into its slot.
func (db *Database) swapRemove(i int) {
	last := len(db.dirs) - 1
	db.dirs[i] = db.dirs[last]
	db.dirs[last] = Dir{}
	db.dirs = db.dirs[:last]
	db.dirty = true
}

// Age rescales and prunes when the total raw rank exceeds maxAge: scale every
// rank by 0.9*maxAge/total (deliberate undershoot), then drop entries whose
// post-scaling rank falls below 1.0 (R-DB-4). Mirrors Database::age.
func (db *Database) Age(maxAge Rank) {
	var total Rank
	for i := range db.dirs {
		total += db.dirs[i].Rank
	}
	if total <= maxAge {
		return
	}
	factor := 0.9 * maxAge / total
	for i := len(db.dirs) - 1; i >= 0; i-- {
		db.dirs[i].Rank *= factor
		// Aliases are rescaled like everything else but exempt from the cull:
		// they are deleted only by `alias rm` (§2, R2-DB-4).
		if db.dirs[i].Rank < 1.0 && !db.dirs[i].IsAlias() {
			db.swapRemove(i)
		}
	}
	db.dirty = true
}

// Dedup merges adjacent-equal entries after sorting by the identity key
// (isAlias, key) where key is the name for aliases and the path otherwise (§3):
//
//   - Equal non-alias keys: rank = sum, last_accessed = max, kind = KindRepo if
//     either is a repo (the upgrade wins).
//   - Equal alias keys (same name): rank = sum, last_accessed = max, target
//     path = the entry with the greater last_accessed (latest target wins).
//
// Not reachable in the parity scope — add/add_update/PutAlias avoid duplicates
// proactively; retained for future `import` work (R-DB-5, F-3). Mirrors
// Database::dedup.
func (db *Database) Dedup() {
	db.sortByIdentity()
	merged := false
	for i := len(db.dirs) - 1; i >= 1; i-- {
		prev, cur := &db.dirs[i-1], &db.dirs[i]
		if prev.IsAlias() != cur.IsAlias() || dedupKey(prev) != dedupKey(cur) {
			continue
		}
		if cur.IsAlias() && cur.LastAccessed > prev.LastAccessed {
			// Latest target wins for aliases.
			prev.Path = cur.Path
		}
		if !cur.IsAlias() && cur.Kind == KindRepo {
			prev.Kind = KindRepo
		}
		if cur.LastAccessed > prev.LastAccessed {
			prev.LastAccessed = cur.LastAccessed
		}
		prev.Rank += cur.Rank
		db.swapRemove(i)
		merged = true
	}
	if merged {
		db.dirty = true
	}
}

// dedupKey is the identity key used by Dedup's sort: the name for an alias, the
// path otherwise (§3).
func dedupKey(d *Dir) string {
	if d.IsAlias() {
		return d.Name
	}
	return d.Path
}

// sortByIdentity orders entries by (isAlias, key) so equal-identity entries are
// adjacent for Dedup. Non-dirtying (D-4).
func (db *Database) sortByIdentity() {
	sort.SliceStable(db.dirs, func(i, j int) bool {
		ai, aj := db.dirs[i].IsAlias(), db.dirs[j].IsAlias()
		if ai != aj {
			return !ai // non-aliases first
		}
		return dedupKey(&db.dirs[i]) < dedupKey(&db.dirs[j])
	})
}

// SortByPath sorts entries by path (byte-wise). Deliberately does NOT set dirty:
// on-disk order is irrelevant to correctness, so we never rewrite the file just
// to persist an ordering (deviation D-4). Uses a stable sort for determinism.
func (db *Database) SortByPath() {
	sort.SliceStable(db.dirs, func(i, j int) bool {
		return db.dirs[i].Path < db.dirs[j].Path
	})
}

// SortByScore sorts entries ascending by decayed score (so callers iterate in
// reverse for best-first). Does NOT set dirty (deviation D-4). Ties are broken
// by path for a deterministic total order in place of Rust's f64::total_cmp on
// equal scores.
func (db *Database) SortByScore(now Epoch) {
	sort.SliceStable(db.dirs, func(i, j int) bool {
		si, sj := db.dirs[i].Score(now), db.dirs[j].Score(now)
		if si != sj {
			return si < sj
		}
		return db.dirs[i].Path < db.dirs[j].Path
	})
}

// findPath returns the dir/repo entry with the given path, skipping aliases (an
// alias may share a path with a real entry but is a separate identity — §3).
func (db *Database) findPath(path string) *Dir {
	for i := range db.dirs {
		if db.dirs[i].Kind != KindAlias && db.dirs[i].Path == path {
			return &db.dirs[i]
		}
	}
	return nil
}

// findAlias returns the alias entry with the given name (names are unique).
func (db *Database) findAlias(name string) *Dir {
	for i := range db.dirs {
		if db.dirs[i].Kind == KindAlias && db.dirs[i].Name == name {
			return &db.dirs[i]
		}
	}
	return nil
}

// upgradeKind applies the dir->repo one-way upgrade (§2): the result is KindRepo
// if either kind is a repo, and repo is never downgraded to dir. Only defined for
// path-entry kinds (KindDir/KindRepo); aliases never flow through here.
func upgradeKind(existing, incoming Kind) Kind {
	if existing == KindRepo || incoming == KindRepo {
		return KindRepo
	}
	return existing
}

func max0(r Rank) Rank {
	if r < 0.0 {
		return 0.0
	}
	return r
}
