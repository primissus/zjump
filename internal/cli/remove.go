package cli

import (
	"fmt"

	"github.com/primissus/zjump/internal/paths"
)

// runRemove implements `zjump remove`. It never reads the clock, so it has no
// clock-error case (R-ERR-4). For each path it tries an exact match, then a
// lexically-resolved match (R-RM-1/2/3). Mirrors cmd/remove.rs.
func runRemove(args []string) error {
	fs := newFlagSet("remove")
	targets, err := parseArgs(fs, args)
	if err != nil {
		return err
	}

	database, err := openDB()
	if err != nil {
		return err
	}

	for _, target := range targets {
		if database.Remove(target) {
			continue
		}
		resolved, err := paths.ResolvePath(target)
		if err != nil {
			return err
		}
		// If resolving was a no-op (already absolute) or the retry also fails,
		// the path simply isn't in the database.
		if resolved == target || !database.Remove(resolved) {
			return fmt.Errorf("path not found in database: %s", target)
		}
	}

	return database.Save()
}
