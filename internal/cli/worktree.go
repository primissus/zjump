package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zjump/internal/errs"
	"zjump/internal/git"
)

// runWorktree implements `zjump worktree <name> [repo-keywords...]`. It matches
// <name> against worktree directory basenames first, then against branch
// shortnames, and prints the path of the matched worktree.
func runWorktree(args []string) error {
	fs := newFlagSet("worktree")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("worktree: worktree name required")
	}
	name := rest[0]
	repoKW := rest[1:]

	repoDir, err := resolveRepo(repoKW)
	if err != nil {
		return err
	}

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
	return fmt.Errorf("no worktree found: %s%s", name, hint)
}
