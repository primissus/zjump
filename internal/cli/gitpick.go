package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/fzf"
	"github.com/primissus/zjump/internal/git"
	"github.com/primissus/zjump/internal/paths"
)

type gitEntry struct {
	label string
	path  string
}

func gitFzfPickAndPrint(entries []gitEntry) error {
	if len(entries) == 0 {
		return fmt.Errorf("no matches found")
	}
	if len(entries) == 1 {
		_, werr := fmt.Fprintln(os.Stdout, entries[0].path)
		return errs.PipeExit(werr, "stdout")
	}

	f, err := fzf.New()
	if err != nil {
		return err
	}
	if opts, ok := config.FzfOpts(); ok {
		f.Env("FZF_DEFAULT_OPTS", opts)
	} else {
		f.StdAppearance()
		f.EnablePreview()
	}
	f.WithNth(1)

	child, err := f.Spawn()
	if err != nil {
		return err
	}

	var selection string
	for _, e := range entries {
		sel, werr := child.Write(e.label + "\t" + e.path)
		if werr != nil {
			return werr
		}
		if sel != nil {
			selection = *sel
			break
		}
	}
	if selection == "" {
		sel, werr := child.Wait()
		if werr != nil {
			return werr
		}
		selection = sel
	}

	_, path, found := strings.Cut(strings.TrimSpace(selection), "\t")
	if !found || path == "" {
		return fmt.Errorf("could not read selection from fzf")
	}
	_, werr := fmt.Fprintln(os.Stdout, path)
	return errs.PipeExit(werr, "stdout")
}

func collectBranchEntries(repoDir string) ([]gitEntry, error) {
	wts, err := git.Worktrees(repoDir)
	if err != nil {
		return nil, err
	}
	var entries []gitEntry
	for _, wt := range wts {
		if wt.Detached || wt.Branch == "" {
			continue
		}
		entries = append(entries, gitEntry{label: wt.Branch, path: wt.Path})
	}
	return entries, nil
}

func collectWorktreeEntries(repoDir string) ([]gitEntry, error) {
	wts, err := git.Worktrees(repoDir)
	if err != nil {
		return nil, err
	}
	var entries []gitEntry
	for _, wt := range wts {
		label := filepath.Base(wt.Path)
		if wt.Branch != "" && label != wt.Branch {
			label = fmt.Sprintf("%s (%s)", label, wt.Branch)
		}
		entries = append(entries, gitEntry{label: label, path: wt.Path})
	}
	return entries, nil
}

func scanDBForWorktrees(database *db.Database, n int) ([]gitEntry, error) {
	now, err := paths.CurrentTime()
	if err != nil {
		return nil, err
	}
	excludeGlobs, err := config.ExcludeDirs()
	if err != nil {
		return nil, err
	}

	opts := db.NewStreamOptions(now).
		WithExclude(excludeGlobs).
		WithExists(true).
		WithResolveSymlinks(config.ResolveSymlinks())
	stream := db.NewStream(database, opts)

	var entries []gitEntry
	for {
		dir := stream.Next()
		if dir == nil || len(entries) >= n {
			break
		}
		gitf := filepath.Join(dir.Path, ".git")
		if _, statErr := os.Stat(gitf); statErr != nil {
			continue
		}
		branch := git.CurrentBranch(dir.Path)
		label := branch
		if label == "" || label == "HEAD" {
			label = filepath.Base(dir.Path)
		}
		entries = append(entries, gitEntry{label: label, path: dir.Path})
	}
	return entries, nil
}

func pickFromDBBranch() error {
	database, err := openDB()
	if err != nil {
		return err
	}
	defer database.Save()

	n, err := config.PickTop()
	if err != nil {
		return err
	}
	entries, err := scanDBForWorktrees(database, n)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no git worktrees found in the tracked directories")
	}
	return gitFzfPickAndPrint(entries)
}

func pickFromDBWorktree() error {
	database, err := openDB()
	if err != nil {
		return err
	}
	defer database.Save()

	n, err := config.PickTop()
	if err != nil {
		return err
	}
	entries, err := scanDBForWorktrees(database, n)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no git worktrees found in the tracked directories")
	}
	for i, e := range entries {
		base := filepath.Base(e.path)
		if e.label != "" && e.label != "HEAD" && e.label != base {
			entries[i].label = fmt.Sprintf("%s (%s)", base, e.label)
		} else {
			entries[i].label = base
		}
	}
	return gitFzfPickAndPrint(entries)
}
