package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/git"
	"github.com/primissus/zjump/internal/log"
	"github.com/primissus/zjump/internal/paths"
)

const addHelp = `Usage: zjump add [--score SCORE] <paths>...

Add one or more directories to the frecency database, or increment
their rank. Paths that match _ZJUMP_EXCLUDE_DIRS are silently skipped.

Flags:
    -s, --score SCORE    Rank increment (default 1.0)
`

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
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "add", addHelp)
			return nil
		}
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
	autoIndex := config.AutoIndexDirectory()
	log.Debugf("add: targets=%v (score=%.1f, autoIndex=%t)", targets, score, autoIndex)

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
			log.Errorf("invalid unicode in path: %s", resolved)
			return fmt.Errorf("invalid unicode in path: %s", resolved)
		}

		// Silently skip newline/CR-bearing or excluded paths (R-ADD-3/4).
		if strings.ContainsAny(resolved, excludeChars) || matchesAny(excludeDirs, resolved) {
			continue
		}
		if info, statErr := os.Stat(resolved); statErr != nil || !info.IsDir() {
			log.Errorf("not a directory: %s", resolved)
			return fmt.Errorf("not a directory: %s", resolved)
		}

		// D-6: a repo root is typed KindRepo (auto-typed on add, G-4); an
		// existing dir entry is upgraded in place, never downgraded (PLAN-GIT §2).
		kind := db.KindDir
		if git.IsRepoRoot(resolved) {
			kind = db.KindRepo
		}
		database.AddUpdate(resolved, score, now, kind)
		log.Debugf("add: added %s (score=%.1f, kind=%d)", resolved, score, kind)

		// _ZJUMP_AUTO_INDEX_DIRECTORY=1: seed the worktrees (and branches) of
		// the repository containing this path, once each, so they become
		// jumpable by frecency without a prior visit. Best effort — git
		// errors are swallowed by seedWorktrees.
		if autoIndex {
			repoDir, repoErr := git.RepoRoot(resolved)
			if repoErr == nil {
				seedWorktrees(database, repoDir, now)
			}
		}
	}

	// Aging runs only if something actually changed (R-ADD-8).
	if database.Dirty() {
		database.Age(maxAge)
	}
	return database.Save()
}
