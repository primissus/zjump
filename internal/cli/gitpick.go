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
	"github.com/primissus/zjump/internal/log"
	"github.com/primissus/zjump/internal/paths"
)

type gitEntry struct {
	label string
	path  string
}

func gitFzfPickAndPrint(entries []gitEntry) error {
	if len(entries) == 0 {
		log.Errorf("no matches found")
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
		log.Errorf("could not read selection from fzf")
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

// seedWorktrees seeds every worktree path of repoDir into the frecency database
// exactly once: a path already present is left alone (so its rank is only ever
// driven by real visits), a missing path is inserted with rank 1.0 so it stays
// queryable and survives aging. git errors are swallowed — indexing is best
// effort and must never fail an `add` or a `worktree`/`branch` lookup.
func seedWorktrees(database *db.Database, repoDir string, now db.Epoch) {
	wts, err := git.Worktrees(repoDir)
	if err != nil {
		return
	}
	for _, wt := range wts {
		if wt.Path == "" {
			continue
		}
		if info, statErr := os.Stat(wt.Path); statErr != nil || !info.IsDir() {
			continue
		}
		if !database.Contains(wt.Path) {
			database.Add(wt.Path, 1.0, now, db.KindDir)
		}
	}
}

// seedRepoWorktrees seeds the worktrees (and branches) of repoDir into the
// frecency database once each. It is best effort: a failure to open the
// database (bad _ZJUMP_DATA_DIR) only skips indexing, never fails the
// `worktree`/`branch` lookup that triggered it.
func seedRepoWorktrees(repoDir string) {
	database, err := openDB()
	if err != nil {
		return
	}
	// H3: surface persistence failures instead of silently discarding them.
	defer func() {
		if err := database.Save(); err != nil {
			log.Errorf("save failed: %v", err)
		}
	}()
	now, err := paths.CurrentTime()
	if err != nil {
		return
	}
	seedWorktrees(database, repoDir, now)
}

// collectAllReposWorktrees scans the DB directories (optionally narrowed by
// keywords) for a `.git` entry and collects the worktrees of each distinct repo
// (deduped by canonical main checkout path, mirroring list.go's
// collectAllReposRows). Each entry's label carries a repo disambiguator so
// same-named worktrees across repos stay distinguishable in the fzf picker.
// The enumeration is intentionally bounded to maxWorktreeRepos repositories
// (shared with query --type worktree) to cap git subprocesses on fat databases.
func collectAllReposWorktrees(database *db.Database, now db.Epoch, keywords []string) ([]gitEntry, error) {
	excludeGlobs, err := config.ExcludeDirs()
	if err != nil {
		return nil, err
	}
	opts := db.NewStreamOptions(now).
		WithKeywords(keywords).
		WithExclude(excludeGlobs).
		WithExists(true).
		WithResolveSymlinks(config.ResolveSymlinks())
	stream := db.NewStream(database, opts)

	seen := make(map[string]bool)
	var entries []gitEntry
	probed := 0
	for {
		dir := stream.Next()
		if dir == nil {
			break
		}
		if _, statErr := os.Stat(filepath.Join(dir.Path, ".git")); statErr != nil {
			continue
		}
		if probed >= maxWorktreeRepos {
			break
		}
		probed++
		wts, werr := git.Worktrees(dir.Path)
		if werr != nil || len(wts) == 0 {
			continue
		}
		canonical := wts[0].Path
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		for _, wt := range wts {
			label := filepath.Base(wt.Path)
			if wt.Branch != "" && label != wt.Branch {
				label = fmt.Sprintf("%s (%s)", label, wt.Branch)
			}
			label = fmt.Sprintf("%s [repo: %s]", label, filepath.Base(canonical))
			entries = append(entries, gitEntry{label: label, path: wt.Path})
		}
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

// pickFromDB is the shared fallback skeleton for pickFromDBBranch and
// pickFromDBWorktree (M3): open the DB, scan the top-N entries, and fzf-pick.
// Only the tail label reformat differs, selected via worktreeLabels.
func pickFromDB(worktreeLabels bool) error {
	database, err := openDB()
	if err != nil {
		return err
	}
	// H3: surface persistence failures instead of silently discarding them.
	defer func() {
		if err := database.Save(); err != nil {
			log.Errorf("save failed: %v", err)
		}
	}()

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
	if worktreeLabels {
		for i, e := range entries {
			base := filepath.Base(e.path)
			if e.label != "" && e.label != "HEAD" && e.label != base {
				entries[i].label = fmt.Sprintf("%s (%s)", base, e.label)
			} else {
				entries[i].label = base
			}
		}
	}
	return gitFzfPickAndPrint(entries)
}

func pickFromDBBranch() error {
	return pickFromDB(false)
}

func pickFromDBWorktree() error {
	return pickFromDB(true)
}
