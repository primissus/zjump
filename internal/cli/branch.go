package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/git"
	"github.com/primissus/zjump/internal/paths"
)

// runBranch implements `zjump branch <branch> [repo-keywords...]`. It finds a
// worktree (including the main checkout) that has <branch> checked out and
// prints its path so the shell can cd into it.
func runBranch(args []string) error {
	fs := newFlagSet("branch")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return runBranchPick(rest[1:]) // rest is empty, so repoKW=[]
	}
	branch := rest[0]
	repoKW := rest[1:]

	repoDir, err := resolveRepo(repoKW)
	if err != nil {
		return err
	}

	wts, err := git.Worktrees(repoDir)
	if err != nil {
		return fmt.Errorf("could not list worktrees: %w", err)
	}

	// Find the worktree whose branch shortname matches.
	var matched []git.Worktree
	for _, wt := range wts {
		if wt.Branch == branch {
			matched = append(matched, wt)
		}
	}
	if len(matched) == 0 {
		var found []string
		for _, wt := range wts {
			if wt.Branch != "" {
				found = append(found, wt.Branch)
			}
		}
		hint := ""
		if len(found) > 0 {
			hint = fmt.Sprintf("\navailable branches: %s", strings.Join(found, ", "))
		}
		return fmt.Errorf("branch not checked out in any worktree: %s%s", branch, hint)
	}
	if len(matched) > 1 {
		// Multiple worktrees have this branch — ambiguous; list them.
		var paths []string
		for _, wt := range matched {
			paths = append(paths, wt.Path)
		}
		return fmt.Errorf("branch %s is checked out in multiple worktrees: %s", branch, strings.Join(paths, ", "))
	}

	if _, werr := fmt.Fprintln(os.Stdout, matched[0].Path); werr != nil {
		return errs.PipeExit(werr, "stdout")
	}
	return nil
}

func runBranchPick(repoKW []string) error {
	if len(repoKW) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not get current working directory: %w", err)
		}
		repoDir, repoErr := git.RepoRoot(cwd)
		if repoErr != nil {
			return pickFromDBBranch()
		}
		entries, err := collectBranchEntries(repoDir)
		if err != nil {
			return fmt.Errorf("could not list branches: %w", err)
		}
		return gitFzfPickAndPrint(entries)
	}
	repoDir, err := resolveRepo(repoKW)
	if err != nil {
		return err
	}
	entries, err := collectBranchEntries(repoDir)
	if err != nil {
		return fmt.Errorf("could not list branches: %w", err)
	}
	return gitFzfPickAndPrint(entries)
}

// resolveRepo resolves the target repository directory. If keywords are given
// they are matched against the frecency database (the resolved path must be
// inside a git repo); otherwise the current working directory is used.
func resolveRepo(keywords []string) (string, error) {
	if len(keywords) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("could not get current working directory: %w", err)
		}
		return git.RepoRoot(cwd)
	}

	// Query the DB with the given keywords to find a candidate directory.
	database, err := openDB()
	if err != nil {
		return "", err
	}
	defer database.Save()

	now, clockErr := paths.CurrentTime()
	if clockErr != nil {
		return "", clockErr
	}
	excludeGlobs, globErr := config.ExcludeDirs()
	if globErr != nil {
		return "", globErr
	}
	opts := db.NewStreamOptions(now).
		WithKeywords(keywords).
		WithExclude(excludeGlobs).
		WithExists(true).
		WithResolveSymlinks(config.ResolveSymlinks())
	stream := db.NewStream(database, opts)

	dir := stream.Next()
	if dir == nil {
		return "", fmt.Errorf("no match found for repo: %s", strings.Join(keywords, " "))
	}
	return git.RepoRoot(filepath.Clean(dir.Path))
}
