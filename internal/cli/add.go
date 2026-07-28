package cli

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/paths"
)

// runAdd implements `zjump add`. It loads exclude/maxage config and reads the
// clock BEFORE opening the DB, so a malformed env var or bad clock fails fast
// even if every path would be skipped (R-ADD-7/9). Mirrors cmd/add.rs.
func runAdd(args []string) error {
	// Characters that can't be printed cleanly on one line.
	const excludeChars = "\n\r"

	fs := newFlagSet("add")
	var score float64
	// Default 1.0 gives exactly zoxide's `score.unwrap_or(1.0)` semantics:
	// unset -> 1.0, and `-s 0` -> 0.0 (R-ADD-2).
	fs.Float64Var(&score, "s", 1.0, "rank increment (default 1.0)")
	fs.Float64Var(&score, "score", 1.0, "rank increment (default 1.0)")

	targets, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("add: at least one path is required")
	}

	excludeDirs, err := config.ExcludeDirs()
	if err != nil {
		return err
	}
	maxAge, err := config.Maxage()
	if err != nil {
		return err
	}
	now, err := paths.CurrentTime()
	if err != nil {
		return err
	}

	database, err := openDB()
	if err != nil {
		return err
	}
	resolveSymlinks := config.ResolveSymlinks()

	for _, target := range targets {
		var resolved string
		if resolveSymlinks {
			resolved, err = paths.Canonicalize(target)
		} else {
			resolved, err = paths.ResolvePath(target)
		}
		if err != nil {
			return err
		}
		if !utf8.ValidString(resolved) {
			return fmt.Errorf("invalid unicode in path: %s", resolved)
		}

		// Silently skip newline/CR-bearing or excluded paths (R-ADD-3/4).
		if strings.ContainsAny(resolved, excludeChars) || matchesAny(excludeDirs, resolved) {
			continue
		}
		if info, statErr := os.Stat(resolved); statErr != nil || !info.IsDir() {
			return fmt.Errorf("not a directory: %s", resolved)
		}

		database.AddUpdate(resolved, score, now)
	}

	// Aging runs only if something actually changed (R-ADD-8).
	if database.Dirty() {
		database.Age(maxAge)
	}
	return database.Save()
}
