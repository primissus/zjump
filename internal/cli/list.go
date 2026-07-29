package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/primissus/zjump/internal/alias"
	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/git"
	"github.com/primissus/zjump/internal/log"
	"github.com/primissus/zjump/internal/paths"
)

// runList implements `zjump list`, a zjump-only extension that prints the
// frecency database (DIRECTORIES section by default) with opt-in sections for
// ALIASES, BRANCHES, and WORKTREES. There is no zoxide equivalent — zoxide's
// closest behavior is `zoxide query --list` (a flag, not a subcommand); the
// git sections have no analog at all. Recorded in the extension scope as
// R-LIST-* (REQUIREMENTS.md, DESIGN.md §13.6).
//
// Per deviation D-4 the DB is rewritten only when actually dirty: lazy
// deletions during Stream iteration may set dirty, and the unconditional
// database.Save() at the end is a no-op when clean (mirrors query.go:65).
const listHelp = `Usage: zjump list [OPTIONS] [keywords...]

List directories (zoxide-like) plus opt-in sections for aliases,
branches, and worktrees. This is a zjump-only extension.

Flags:
    -a, --all             Include nonexistent paths
    -s, --score           Print the frecency score alongside the path
    --json                Output in JSON format
    --aliases             Include the ALIASES section
    --branches            Include the BRANCHES section
    --worktrees           Include the WORKTREES section
    --all-repos           Scan all repos from the database for branches/worktrees
    --no-dirs             Suppress the DIRECTORIES section
`

func runList(args []string) error {
	fs := newFlagSet("list")
	var all, score, jsonOut, allRepos bool
	fs.BoolVar(&all, "a", false, "")
	fs.BoolVar(&all, "all", false, "")
	fs.BoolVar(&score, "s", false, "")
	fs.BoolVar(&score, "score", false, "")
	fs.BoolVar(&jsonOut, "json", false, "")
	fs.BoolVar(&allRepos, "all-repos", false, "")

	var showAliases, showBranches, showWorktrees, noDirs bool
	fs.BoolVar(&showAliases, "aliases", false, "")
	fs.BoolVar(&showBranches, "branches", false, "")
	fs.BoolVar(&showWorktrees, "worktrees", false, "")
	fs.BoolVar(&noDirs, "no-dirs", false, "")

	keywords, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "list", listHelp)
			return nil
		}
		return err
	}

	database, err := openDB()
	if err != nil {
		return err
	}
	defer database.Save()
	log.Debugf("list: keywords=%v all=%v", keywords, all)

	now, err := paths.CurrentTime()
	if err != nil {
		return err
	}
	excludeGlobs, err := config.ExcludeDirs()
	if err != nil {
		return err
	}

	opts := db.NewStreamOptions(now).
		WithKeywords(keywords).
		WithExclude(excludeGlobs)
	if !all {
		opts = opts.WithExists(true).WithResolveSymlinks(config.ResolveSymlinks())
	}

	// Collecting DIRECTORIES lazy-deletes stale/nonexistent/excluded entries
	// as a side effect of Stream.Next() (stream.go:102-111); the deferred
	// Save persists those deletions (D-4: no-op when nothing was pruned).
	stream := db.NewStream(database, opts)
	var dirs []listDir
	for {
		d := stream.Next()
		if d == nil {
			break
		}
		dirs = append(dirs, listDir{
			Path:         d.Path,
			Rank:         d.Rank,
			LastAccessed: d.LastAccessed,
			Score:        clampScore(d.Score(now)),
		})
	}

	report := listReport{
		NoDirs:     noDirs,
		ShowAlias:  showAliases,
		ShowBranch: showBranches,
		ShowWT:     showWorktrees,
		AllRepos:   allRepos,
	}
	report.Directories = dirs

	if showAliases {
		report.Aliases = collectAliasEntries()
	}

	if showBranches || showWorktrees {
		if allRepos {
			if _, lookErr := exec.LookPath("git"); lookErr != nil {
				return fmt.Errorf("could not find git, is it installed?")
			}
			report.Branches, report.Worktrees = collectAllReposRows(dirs, showBranches, showWorktrees)
		} else {
			repoDir, rerr := resolveRepoForList(dirs, keywords)
			if rerr != nil {
				// No repo found: leave sections empty (no error) — matches the
				// "CWD repo, else skip" behavior of the plan. rerr is non-nil
				// only when CWD isn't a repo (no keywords) or no DB match
				// (keywords given). In the keywords case we still propagate
				// since the user explicitly named a repo.
				if len(keywords) > 0 {
					return rerr
				}
			} else {
				report.RepoHint = repoDir
				if showBranches {
					report.Branches = collectBranchRows(repoDir)
				}
				if showWorktrees {
					report.Worktrees = collectWorktreeRows(repoDir)
				}
			}
		}
	}

	if jsonOut {
		return printListJSON(os.Stdout, &report)
	}
	return printListText(os.Stdout, &report, score)
}

// listDir is a snapshot of one db.Dir for output. Score is the decayed
// frecency score clamped to [0, 9999] for parity with Dir.DisplayScore
// (internal/db/dir.go:66).
type listDir struct {
	Path         string   `json:"path"`
	Rank         db.Rank  `json:"rank"`
	LastAccessed db.Epoch `json:"last_accessed"`
	Score        db.Rank  `json:"score"`
}

// listAlias is one alias entry (name→path).
type listAlias struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// listBranch is one row of the BRANCHES section. Detached worktrees are
// excluded (mirrors collectBranchEntries at gitpick.go:77-90).
type listBranch struct {
	Repo   string `json:"repo,omitempty"` // empty unless --all-repos
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

// listWorktree is one row of the WORKTREES section (all worktrees,
// including detached ones).
type listWorktree struct {
	Repo     string `json:"repo,omitempty"` // empty unless --all-repos
	Basename string `json:"basename"`
	Branch   string `json:"branch"` // "(detached)" when the worktree's HEAD is detached
	Detached bool   `json:"detached,omitempty"`
	Path     string `json:"path"`
}

// listReport is the assembled view passed to the printers. The Show*/NoDirs/
// AllRepos/RepoHint fields steer output (text + JSON) and are not
// serialized. JSON serialization is gated by the Show*/NoDirs fields plus the
// presence of the slice — see printListJSON, which builds a map keyed by
// requested section so an explicitly-requested-but-empty section appears as
// `[]` (not `null`) and an unrequested section is absent.
type listReport struct {
	Directories []listDir      // always serialized, may be []
	Aliases     []listAlias    // serialized iff ShowAlias
	Branches    []listBranch   // serialized iff ShowBranch
	Worktrees   []listWorktree // serialized iff ShowWT

	NoDirs     bool
	ShowAlias  bool
	ShowBranch bool
	ShowWT     bool
	AllRepos   bool
	RepoHint   string
}

// clampScore mirrors Dir.DisplayScore's clamp (internal/db/dir.go:67) so the
// DIRECTORIES table and JSON agree about the displayed score.
func clampScore(s db.Rank) db.Rank {
	if s < 0.0 {
		return 0.0
	}
	if s > 9999.0 {
		return 9999.0
	}
	return s
}

// collectAliasEntries opens the alias store under the configured data dir and
// returns its sorted entries. Errors are swallowed (no aliases file ⇝ none).
func collectAliasEntries() []listAlias {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil
	}
	store, err := alias.Open(dataDir)
	if err != nil {
		return nil
	}
	entries := store.Entries()
	out := make([]listAlias, 0, len(entries))
	for _, e := range entries {
		out = append(out, listAlias{Name: e.Name, Path: e.Path})
	}
	return out
}

// resolveRepoForList resolves the target repo for the BRANCHES/WORKTREES
// sections in single-repo mode. With no keywords it uses CWD (silently
// errors when not in a repo). With keywords it uses the best DB match (the
// top-ranked dir from the already-collected dirs slice), erroring if the DB
// had no matches — mirrors resolveRepo at branch.go:106-141 without re-opening
// the database.
func resolveRepoForList(dirs []listDir, keywords []string) (string, error) {
	if len(keywords) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("could not get current working directory: %w", err)
		}
		return git.RepoRoot(cwd)
	}
	if len(dirs) == 0 {
		return "", fmt.Errorf("no match found for repo: %s", strings.Join(keywords, " "))
	}
	return git.RepoRoot(filepath.Clean(dirs[0].Path))
}

// collectBranchRows enumerates non-detached worktrees of repoDir as BRANCHES
// rows (no REPO column — the section header carries the repo path).
// Errors are swallowed: a broken repo simply yields no rows.
func collectBranchRows(repoDir string) []listBranch {
	wts, err := git.Worktrees(repoDir)
	if err != nil || len(wts) == 0 {
		return nil
	}
	out := make([]listBranch, 0, len(wts))
	for _, wt := range wts {
		if wt.Detached || wt.Branch == "" {
			continue
		}
		out = append(out, listBranch{Branch: wt.Branch, Path: wt.Path})
	}
	return out
}

// collectWorktreeRows enumerates all worktrees of repoDir as WORKTREES rows.
// Detached worktrees get a "-" branch label so the column stays aligned.
func collectWorktreeRows(repoDir string) []listWorktree {
	wts, err := git.Worktrees(repoDir)
	if err != nil || len(wts) == 0 {
		return nil
	}
	out := make([]listWorktree, 0, len(wts))
	for _, wt := range wts {
		branch := wt.Branch
		detached := wt.Detached
		if detached && branch == "" {
			branch = "(detached)"
		}
		out = append(out, listWorktree{
			Basename: filepath.Base(wt.Path),
			Branch:   branch,
			Detached: detached,
			Path:     wt.Path,
		})
	}
	return out
}

// collectAllReposRows scans every already-collected DIRECTORIES entry for a
// `.git` file/dir and enumerates worktrees across all distinct repos. The
// canonical repo id (first worktree's path, which is the main checkout in
// `git worktree list --porcelain` output) is used as the REPO column and as
// the dedup key so a multi-worktree repo only contributes one set of rows.
// Per-dir git errors are swallowed, per the extension spec.
func collectAllReposRows(dirs []listDir, wantBranches, wantWorktrees bool) (branches []listBranch, worktrees []listWorktree) {
	seen := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		if _, statErr := os.Stat(filepath.Join(d.Path, ".git")); statErr != nil {
			continue
		}
		wts, werr := git.Worktrees(d.Path)
		if werr != nil || len(wts) == 0 {
			continue
		}
		canonical := wts[0].Path
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		for _, wt := range wts {
			if wantBranches && !wt.Detached && wt.Branch != "" {
				branches = append(branches, listBranch{
					Repo:   canonical,
					Branch: wt.Branch,
					Path:   wt.Path,
				})
			}
			if wantWorktrees {
				branch := wt.Branch
				detached := wt.Detached
				if detached && branch == "" {
					branch = "(detached)"
				}
				worktrees = append(worktrees, listWorktree{
					Repo:     canonical,
					Basename: filepath.Base(wt.Path),
					Branch:   branch,
					Detached: detached,
					Path:     wt.Path,
				})
			}
		}
	}
	return branches, worktrees
}

// printListText writes the human-readable, tab-aligned table to w. Empty
// requested sections print their header plus a single "(none)" row so the
// user can distinguish "I asked for aliases and have none" from a suppressed
// section. Unrequested sections are omitted entirely, as is the DIRECTORIES
// section when --no-dirs is set.
func printListText(w io.Writer, r *listReport, score bool) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	var wroteAny bool

	if !r.NoDirs {
		wroteAny = true
		writeSectionHeader(tw, "DIRECTORIES")
		if score {
			fmt.Fprintln(tw, "  SCORE\tPATH")
		} else {
			fmt.Fprintln(tw, "  PATH")
		}
		if len(r.Directories) == 0 {
			fmt.Fprintln(tw, "  (none)")
		} else {
			for _, d := range r.Directories {
				if score {
					fmt.Fprintf(tw, "  %6.1f\t%s\n", d.Score, d.Path)
				} else {
					fmt.Fprintf(tw, "  %s\n", d.Path)
				}
			}
		}
	}

	if r.ShowAlias {
		if wroteAny {
			fmt.Fprintln(tw)
		}
		wroteAny = true
		writeSectionHeader(tw, "ALIASES")
		fmt.Fprintln(tw, "  NAME\tPATH")
		if len(r.Aliases) == 0 {
			fmt.Fprintln(tw, "  (none)")
		} else {
			for _, a := range r.Aliases {
				fmt.Fprintf(tw, "  %s\t%s\n", a.Name, a.Path)
			}
		}
	}

	if r.ShowBranch {
		if wroteAny {
			fmt.Fprintln(tw)
		}
		wroteAny = true
		if r.AllRepos {
			writeSectionHeader(tw, "BRANCHES (all repos in DB)")
			fmt.Fprintln(tw, "  REPO\tBRANCH\tPATH")
		} else if r.RepoHint != "" {
			writeSectionHeader(tw, fmt.Sprintf("BRANCHES (repo: %s)", r.RepoHint))
			fmt.Fprintln(tw, "  BRANCH\tPATH")
		} else {
			writeSectionHeader(tw, "BRANCHES (no git repository)")
			fmt.Fprintln(tw, "  BRANCH\tPATH")
		}
		if len(r.Branches) == 0 {
			fmt.Fprintln(tw, "  (none)")
		} else {
			for _, b := range r.Branches {
				if r.AllRepos {
					fmt.Fprintf(tw, "  %s\t%s\t%s\n", b.Repo, b.Branch, b.Path)
				} else {
					fmt.Fprintf(tw, "  %s\t%s\n", b.Branch, b.Path)
				}
			}
		}
	}

	if r.ShowWT {
		if wroteAny {
			fmt.Fprintln(tw)
		}
		wroteAny = true
		if r.AllRepos {
			writeSectionHeader(tw, "WORKTREES (all repos in DB)")
			fmt.Fprintln(tw, "  REPO\tBASENAME\tBRANCH\tPATH")
		} else if r.RepoHint != "" {
			writeSectionHeader(tw, fmt.Sprintf("WORKTREES (repo: %s)", r.RepoHint))
			fmt.Fprintln(tw, "  BASENAME\tBRANCH\tPATH")
		} else {
			writeSectionHeader(tw, "WORKTREES (no git repository)")
			fmt.Fprintln(tw, "  BASENAME\tBRANCH\tPATH")
		}
		if len(r.Worktrees) == 0 {
			fmt.Fprintln(tw, "  (none)")
		} else {
			for _, wt := range r.Worktrees {
				if r.AllRepos {
					fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", wt.Repo, wt.Basename, wt.Branch, wt.Path)
				} else {
					fmt.Fprintf(tw, "  %s\t%s\t%s\n", wt.Basename, wt.Branch, wt.Path)
				}
			}
		}
	}

	if err := tw.Flush(); err != nil {
		return errs.PipeExit(err, "stdout")
	}
	return nil
}

// writeSectionHeader writes a section title line and a blank underline of
// matching width, matching an "uppercase header / dashed underline" style
// without depending on any third-party table library.
func writeSectionHeader(tw *tabwriter.Writer, title string) {
	// tabwriter pads column cells but plain text lines (no tabs) pass through,
	// so the title and its underline print as-is. We compute the underline
	// width using the printable title (which is ASCII for our headers).
	fmt.Fprintln(tw, title)
	fmt.Fprintln(tw, strings.Repeat("-", len(title)))
}

// printListJSON writes the structured report to w as one JSON object.
// Sections appear in the output iff they were requested (or, for DIRECTORIES,
// not suppressed via --no-dirs). A requested-but-empty section serializes as
// `[]` (not `null`) so downstream consumers can distinguish "asked for, none
// present" from "section not requested" (which omits the key entirely).
func printListJSON(w io.Writer, r *listReport) error {
	body := map[string]any{}

	dirs := r.Directories
	if dirs == nil {
		dirs = []listDir{}
	}
	body["directories"] = dirs

	if r.ShowAlias {
		a := r.Aliases
		if a == nil {
			a = []listAlias{}
		}
		body["aliases"] = a
	}
	if r.ShowBranch {
		b := r.Branches
		if b == nil {
			b = []listBranch{}
		}
		body["branches"] = b
	}
	if r.ShowWT {
		wts := r.Worktrees
		if wts == nil {
			wts = []listWorktree{}
		}
		body["worktrees"] = wts
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(body); err != nil {
		return errs.PipeExit(err, "stdout")
	}
	return nil
}
