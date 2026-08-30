package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/git"
	"github.com/primissus/zjump/internal/log"
	"github.com/primissus/zjump/internal/paths"
)

const worktreeHelp = `Usage: zjump worktree [<name> [repo-keywords...]] [--all]

Print a worktree path by name or branch.

With a name, match against worktree directory basenames first,
then against branch shortnames, and print the path.

With no arguments, launch an interactive fzf picker listing all
worktrees.

Optionally specify repo-keywords to search the frecency database
for a specific repository.

Flags:
    -a, --all    List worktrees across all repositories known to the
                 frecency database (ignores <name>); zjump extension.
`

// runWorktree implements `zjump worktree <name> [repo-keywords...]`. It matches
// <name> against worktree directory basenames first, then against branch
// shortnames, and prints the path of the matched worktree.
func runWorktree(args []string) error {
	fs := newFlagSet("worktree")
	var allRepos bool
	fs.BoolVar(&allRepos, "a", false, "list worktrees across all repos in the DB")
	fs.BoolVar(&allRepos, "all", false, "list worktrees across all repos in the DB")
	rest, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "worktree", worktreeHelp)
			return nil
		}
		return err
	}
	if allRepos {
		return runWorktreePickAll(rest)
	}
	if len(rest) == 0 {
		return runWorktreePick(nil)
	}
	name := rest[0]
	repoKW := rest[1:]
	log.Debugf("worktree: name=%s repoKW=%v", name, repoKW)

	repoDir, err := resolveRepo(repoKW)
	if err != nil {
		return err
	}

	// Index the repo's worktrees (and branches) into the frecency database so
	// they become jumpable by frecency on later `zz <keyword>` calls.
	seedRepoWorktrees(repoDir)

	wts, err := git.Worktrees(repoDir)
	if err != nil {
		return fmt.Errorf("could not list worktrees: %w", err)
	}

	// Match basename first.
	var byBasename []git.Worktree
	for _, wt := range wts {
		if filepath.Base(wt.Path) == name {
			byBasename = append(byBasename, wt)
		}
	}
	if len(byBasename) == 1 {
		if _, werr := fmt.Fprintln(os.Stdout, byBasename[0].Path); werr != nil {
			return errs.PipeExit(werr, "stdout")
		}
		return nil
	}
	if len(byBasename) > 1 {
		var paths []string
		for _, wt := range byBasename {
			paths = append(paths, wt.Path)
		}
		return fmt.Errorf("ambiguous worktree: %s (matches: %s)", name, strings.Join(paths, ", "))
	}

	// Match branch shortname next.
	var byBranch []git.Worktree
	for _, wt := range wts {
		if wt.Branch == name {
			byBranch = append(byBranch, wt)
		}
	}
	if len(byBranch) == 1 {
		if _, werr := fmt.Fprintln(os.Stdout, byBranch[0].Path); werr != nil {
			return errs.PipeExit(werr, "stdout")
		}
		return nil
	}
	if len(byBranch) > 1 {
		var paths []string
		for _, wt := range byBranch {
			paths = append(paths, wt.Path)
		}
		return fmt.Errorf("ambiguous worktree: %s (matches: %s)", name, strings.Join(paths, ", "))
	}

	// No match — list available worktrees as a hint.
	var available []string
	for _, wt := range wts {
		label := filepath.Base(wt.Path)
		if wt.Branch != "" && wt.Branch != label {
			label = fmt.Sprintf("%s (%s)", label, wt.Branch)
		}
		available = append(available, label)
	}
	sort.Strings(available) // deterministic output
	hint := ""
	if len(available) > 0 {
		hint = fmt.Sprintf("\navailable worktrees: %s", strings.Join(available, ", "))
	}
	log.Errorf("no worktree found: %s%s", name, hint)
	return fmt.Errorf("no worktree found: %s%s", name, hint)
}

func runWorktreePick(repoKW []string) error {
	if len(repoKW) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not get current working directory: %w", err)
		}
		repoDir, repoErr := git.RepoRoot(cwd)
		if repoErr != nil {
			return pickFromDBWorktree()
		}
		seedRepoWorktrees(repoDir)
		entries, err := collectWorktreeEntries(repoDir)
		if err != nil {
			return fmt.Errorf("could not list worktrees: %w", err)
		}
		return gitFzfPickAndPrint(entries)
	}
	repoDir, err := resolveRepo(repoKW)
	if err != nil {
		return err
	}
	seedRepoWorktrees(repoDir)
	entries, err := collectWorktreeEntries(repoDir)
	if err != nil {
		return fmt.Errorf("could not list worktrees: %w", err)
	}
	return gitFzfPickAndPrint(entries)
}

// runWorktreePickAll implements `zjump worktree --all` (the shell `zz -W`):
// it lists worktrees across every repository known to the frecency database,
// seeding any not-yet-indexed worktree paths along the way. Dedupes repos by
// canonical main checkout so a multi-worktree repo contributes one set of rows.
func runWorktreePickAll(keywords []string) error {
	database, err := openDB()
	if err != nil {
		return err
	}
	defer database.Save()

	now, err := paths.CurrentTime()
	if err != nil {
		return err
	}
	// Seed all indexed worktrees so a first `zz -W` also makes them jumpable
	// by frecency (same seed-once semantics as seedWorktrees).
	entries, err := collectAllReposWorktrees(database, now, keywords)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !database.Contains(e.path) {
			database.Add(e.path, 1.0, now, db.KindDir)
		}
	}
	if len(entries) == 0 {
		return fmt.Errorf("no git worktrees found in the tracked directories")
	}
	return gitFzfPickAndPrint(entries)
}
