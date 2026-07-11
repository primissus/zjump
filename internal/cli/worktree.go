package cli

import (
	"fmt"
	"os"
	"strings"

	"zjump/internal/config"
	"zjump/internal/db"
	"zjump/internal/errs"
	"zjump/internal/fzf"
	"zjump/internal/gitx"
	"zjump/internal/glob"
)

// maxWorktreeRepos caps how many repositories the worktree enumeration will
// probe, bounding the number of git subprocesses per query (§6 step 5).
const maxWorktreeRepos = 50

// worktreeLister enumerates a repository's worktrees. It is a package variable
// (defaulting to the real git-backed implementation) so tests can substitute a
// fixture enumerator without a real git.
var worktreeLister = gitx.WorktreeList

// wtCand is one worktree candidate: its path, branch label, and the decayed
// score of the repository that owns it (used for output ordering/display).
type wtCand struct {
	path   string
	branch string
	score  float64
}

// queryWorktree implements `zjump query --type worktree` — the live worktree
// enumeration over indexed repositories (§6, D-6). No git subprocess runs unless
// this type is requested (§6 step 8).
//
// Repositories stream best-score-first through the normal DB machinery (KindRepo
// only, keyword filter disabled here, existence/lazy-pruning applied to repo
// paths). Each repo's worktrees are enumerated via `git worktree list
// --porcelain`, keyword-matched on the worktree path, de-duplicated across repos
// (first occurrence wins), and bounded to the first 50 repos.
func queryWorktree(database *db.Database, p queryParams, now db.Epoch) error {
	excludeGlobs, err := config.ExcludeDirs()
	if err != nil {
		return err
	}

	switch {
	case p.interactive:
		return worktreeInteractive(database, p, now, excludeGlobs)
	case p.list:
		return worktreeList(database, p, now, excludeGlobs)
	default:
		return worktreeFirst(database, p, now, excludeGlobs)
	}
}

// streamWorktrees drives the repo stream and invokes visit for each surviving
// worktree candidate (post keyword-match and cross-repo dedup), in repo-frecency
// order then git output order. visit returns true to stop early (used by the
// default first-match mode so subprocesses run lazily — §6 step 5). The
// --exclude flag is NOT applied here; callers apply it so the default mode can
// distinguish "no match" from "already in the only match".
func streamWorktrees(database *db.Database, p queryParams, now db.Epoch, excludeGlobs []*glob.Glob, visit func(wtCand) bool) {
	opts := db.NewStreamOptions(now).
		WithKinds(db.KindRepo).
		WithExclude(excludeGlobs)
	if !p.all {
		opts = opts.WithExists(true).WithResolveSymlinks(config.ResolveSymlinks())
	}
	stream := db.NewStream(database, opts)

	seen := make(map[string]bool)
	repos := 0
	for {
		repo := stream.Next()
		if repo == nil {
			return
		}
		wts, err := worktreeLister(repo.Path)
		if err == nil {
			score := repo.Score(now)
			for _, w := range wts {
				if seen[w.Path] || !db.MatchPath(p.keywords, w.Path) {
					continue
				}
				seen[w.Path] = true
				if visit(wtCand{path: w.Path, branch: w.Branch, score: score}) {
					return
				}
			}
		}
		repos++
		if repos >= maxWorktreeRepos {
			return
		}
	}
}

// worktreeFirst prints the single best worktree path, honoring --exclude with
// the same "no match found" / "you are already in the only match" contract as a
// normal query (§6 step 6, step 8).
func worktreeFirst(database *db.Database, p queryParams, now db.Epoch, excludeGlobs []*glob.Glob) error {
	var chosen *wtCand
	sawAny := false
	streamWorktrees(database, p, now, excludeGlobs, func(c wtCand) bool {
		sawAny = true
		if p.excludeSet && c.path == p.exclude {
			return false // skip the excluded path, keep looking
		}
		cc := c
		chosen = &cc
		return true // stop at the first surviving candidate
	})

	if chosen == nil {
		if sawAny {
			return fmt.Errorf("you are already in the only match")
		}
		return fmt.Errorf("no match found")
	}
	_, werr := fmt.Fprintln(os.Stdout, formatWorktree(chosen, p.score))
	return errs.PipeExit(werr, "stdout")
}

// worktreeList prints every matching worktree path (score prefix if -s), one per
// line, skipping the --exclude'd path (§6 step 6).
func worktreeList(database *db.Database, p queryParams, now db.Epoch, excludeGlobs []*glob.Glob) error {
	var writeErr error
	streamWorktrees(database, p, now, excludeGlobs, func(c wtCand) bool {
		if p.excludeSet && c.path == p.exclude {
			return false
		}
		if _, werr := fmt.Fprintln(os.Stdout, formatWorktree(&c, p.score)); werr != nil {
			writeErr = errs.PipeExit(werr, "stdout")
			return true
		}
		return false
	})
	return writeErr
}

// worktreeInteractive streams three-field records ({score}\t{path}\t[{branch}])
// to fzf and prints the selected worktree path (field 2). It deliberately does
// not reuse the two-field "strip 7 chars" logic (§6 step 6).
func worktreeInteractive(database *db.Database, p queryParams, now db.Epoch, excludeGlobs []*glob.Glob) error {
	child, err := worktreeFzf()
	if err != nil {
		return err
	}

	var (
		selection string
		selected  bool
		fzfErr    error
	)
	streamWorktrees(database, p, now, excludeGlobs, func(c wtCand) bool {
		if p.excludeSet && c.path == p.exclude {
			return false
		}
		record := fmt.Sprintf("%6.1f\t%s\t[%s]", clampScore(c.score), c.path, c.branch)
		sel, werr := child.Write(record)
		if werr != nil {
			fzfErr = werr
			return true
		}
		if sel != nil {
			selection, selected = *sel, true
			return true
		}
		return false
	})
	if fzfErr != nil {
		return fzfErr
	}
	if !selected {
		sel, werr := child.Wait()
		if werr != nil {
			return werr
		}
		selection = sel
	}

	// Extract field 2 (the path) from "{score}\t{path}\t[{branch}]".
	fields := strings.SplitN(strings.TrimRight(selection, "\n"), "\t", 3)
	if len(fields) < 2 {
		return fmt.Errorf("could not read selection from fzf")
	}
	_, werr := fmt.Fprintln(os.Stdout, fields[1])
	return errs.PipeExit(werr, "stdout")
}

// formatWorktree renders a worktree candidate for default/list output: the path
// alone, or a score-prefixed line under -s (score = the owning repo's decayed
// score — §6 step 6).
func formatWorktree(c *wtCand, score bool) string {
	if score {
		return fmt.Sprintf("%6.1f %s", clampScore(c.score), c.path)
	}
	return c.path
}

// worktreeFzf builds the interactive fzf child for worktree selection. The base
// args already match on field 2 (the path); the branch column is visible but not
// matchable. No preview: the three-field record would confuse the {2..} preview
// placeholder used elsewhere.
func worktreeFzf() (*fzf.Child, error) {
	f, err := fzf.New()
	if err != nil {
		return nil, err
	}
	f.Args(
		"--exact",
		"--no-sort",
		"--bind=ctrl-z:ignore,btab:up,tab:down",
		"--cycle",
		"--keep-right",
		"--border=sharp",
		"--height=45%",
		"--info=inline",
		"--layout=reverse",
		"--tabstop=1",
		"--exit-0",
	)
	return f.Spawn()
}
