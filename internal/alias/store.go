// Package alias stores named aliases (shortcuts to directories) in a zjump-native
// versioned binary file alongside the frecency database. The store supports the
// `z <alias>` jump resolution and the `zjump alias` subcommand.
package alias

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/primissus/zjump/internal/atomic"
)

const filename = "aliases"

// Store is an in-memory view of the aliases file plus a dirty flag.
type Store struct {
	path    string
	entries map[string]string // name → path
	dirty   bool
}

// Open opens (or lazily initialises) the alias store under dataDir. The data
// directory is created eagerly; the file itself is not written until the first
// Save with dirty data.
func Open(dataDir string) (*Store, error) {
	path := filepath.Join(dataDir, filename)
	if resolved, err := filepath.Abs(path); err == nil {
		path = resolved
	}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		entries, derr := deserialize(data)
		if derr != nil {
			return nil, derr
		}
		m := make(map[string]string, len(entries))
		for _, e := range entries {
			m[e.Name] = e.Path
		}
		return &Store{path: path, entries: m}, nil
	case errors.Is(err, fs.ErrNotExist):
		if mkErr := os.MkdirAll(dataDir, 0o700); mkErr != nil {
			return nil, fmt.Errorf("unable to create data directory: %s: %w", dataDir, mkErr)
		}
		return &Store{path: path, entries: make(map[string]string)}, nil
	default:
		return nil, fmt.Errorf("could not read aliases: %s: %w", path, err)
	}
}

// Get returns the path for name, if it exists. Case-sensitive exact match.
func (s *Store) Get(name string) (string, bool) {
	p, ok := s.entries[name]
	return p, ok
}

// Set creates or overwrites an entry for name. It is the caller's responsibility
// to validate the name format and that the path exists as a directory.
func (s *Store) Set(name, path string) {
	if existing, ok := s.entries[name]; ok && existing == path {
		return // no change
	}
	s.entries[name] = path
	s.dirty = true
}

// Delete removes name. Returns false if the name was not present.
func (s *Store) Delete(name string) bool {
	if _, ok := s.entries[name]; !ok {
		return false
	}
	delete(s.entries, name)
	s.dirty = true
	return true
}

// Entries returns a sorted list of all alias entries.
func (s *Store) Entries() []Alias {
	entries := make([]Alias, 0, len(s.entries))
	for name, path := range s.entries {
		entries = append(entries, Alias{Name: name, Path: path})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// Dirty reports whether there are unsaved changes.
func (s *Store) Dirty() bool { return s.dirty }

// Save atomically rewrites the file, but only when dirty (a no-op otherwise).
func (s *Store) Save() error {
	if !s.dirty {
		return nil
	}
	data, err := serialize(s.Entries())
	if err != nil {
		return fmt.Errorf("could not serialize aliases: %w", err)
	}
	if err := atomic.Write(s.path, data); err != nil {
		return fmt.Errorf("could not write aliases: %w", err)
	}
	s.dirty = false
	return nil
}

// ValidateName checks that name is a valid alias name: non-empty, no /, no
// newline / carriage-return, not "." or "..", and does not start with "-".
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("alias name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("alias name must not be %q", name)
	}
	if name[0] == '-' {
		return fmt.Errorf("alias name must not start with '-'")
	}
	if strings.ContainsAny(name, "\n\r/") {
		return fmt.Errorf("alias name must not contain newline, carriage-return, or /")
	}
	return nil
}
