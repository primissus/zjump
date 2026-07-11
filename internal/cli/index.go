package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zjump/internal/config"
	"zjump/internal/db"
	"zjump/internal/gitx"
	"zjump/internal/paths"
)

// runIndex implements `zjump index [--max-depth N] [--score S] <ROOT>...` — a
// bulk repository scanner zoxide has no analogue for (G-4, D-6). It walks each
// root top-down, records git repo roots (without descending into them), and
// upserts each as a KindRepo entry, then runs one aging pass and a single save
// (PLAN-GIT.md §5.3, R2-IDX-3).
func runIndex(args []string) error {
	fs := newFlagSet("index")
	var maxDepth int
	fs.IntVar(&maxDepth, "max-depth", 3, "maximum directory depth to descend (root is depth 0)")
	var score float64
	fs.Float64Var(&score, "score", 1.0, "rank to add for each indexed repository")

	roots, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return fmt.Errorf("index: at least one root directory is required")
	}

	// Load config + clock before opening the DB, matching `add` (R-ADD-7/9).
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

	// Validate every root up front (existence + directory) so a bad root fails
	// fast, consistent with `add`'s "not a directory" contract.
	for _, root := range roots {
		if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
			return fmt.Errorf("not a directory: %s", root)
		}
	}

	database, err := openDB()
	if err != nil {
		return err
	}
	resolveSymlinks := config.ResolveSymlinks()

	indexed := 0
	record := func(repoPath string) error {
		var (
			resolved string
			rerr     error
		)
		if resolveSymlinks {
			resolved, rerr = paths.Canonicalize(repoPath)
		} else {
			resolved, rerr = paths.ResolvePath(repoPath)
		}
		if rerr != nil {
			return rerr
		}
		// Same skip rules as `add` (§5.3): newline/CR-bearing or excluded paths
		// are silently dropped.
		if strings.ContainsAny(resolved, "\n\r") || matchesAny(excludeDirs, resolved) {
			return nil
		}
		database.AddUpdate(resolved, score, now, db.KindRepo)
		indexed++
		return nil
	}

	for _, root := range roots {
		if err := walkForRepos(root, 0, maxDepth, record); err != nil {
			return err
		}
	}

	// Aging + a single save, mirroring `add` (§5.3).
	if database.Dirty() {
		database.Age(maxAge)
	}
	if err := database.Save(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "indexed %d repositories under %d roots\n", indexed, len(roots))
	return nil
}

// walkForRepos recurses dir top-down looking for repo roots. A repo root is
// recorded and NOT descended into (nested repos/submodules are out of scope). A
// non-repo directory is recursed into unless it is already at maxDepth. Hidden
// directories (name starting with "."), symlinks, and unreadable directories are
// skipped; the initial root argument is exempt from the hidden rule since only
// child entry names are tested (§5.3).
func walkForRepos(dir string, depth, maxDepth int, record func(string) error) error {
	if gitx.IsRepoRoot(dir) {
		return record(dir)
	}
	if depth >= maxDepth {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // unreadable directory: skip silently (permission errors etc.)
	}
	for _, e := range entries {
		// Only descend into real directories: DirEntry.IsDir() is false for a
		// symlink, so symlinks are never followed.
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue // hidden directory
		}
		if err := walkForRepos(filepath.Join(dir, e.Name()), depth+1, maxDepth, record); err != nil {
			return err
		}
	}
	return nil
}
