package cli

import (
	"fmt"
	"os"
	"strings"

	"zjump/internal/errs"
	"zjump/internal/fzf"
	"zjump/internal/gitx"
)

// runBranch implements `zjump branch [PATTERN]` — interactive local-branch
// selection in the current repository, a capability zoxide lacks (G-3, D-6). It
// prints the chosen branch name to stdout; the generated shell function runs the
// actual `git switch` (§5.5, §7). The binary never switches branches itself.
func runBranch(args []string) error {
	fs := newFlagSet("branch")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("branch: at most one PATTERN argument is allowed")
	}
	var pattern string
	if len(rest) == 1 {
		pattern = rest[0]
	}

	if !gitx.InsideWorkTree() {
		return fmt.Errorf("not inside a git repository")
	}
	branches, err := gitx.LocalBranches()
	if err != nil {
		return err
	}
	if len(branches) == 0 {
		return fmt.Errorf("no branches found")
	}

	// Move the current branch to the front and remember it for the marker column.
	current := gitx.CurrentBranch()
	if current != "" {
		branches = moveToFront(branches, current)
	}

	// Fast path: a PATTERN that case-sensitively substring-matches exactly one
	// branch is printed directly — no fzf, so it is scriptable and works without
	// fzf installed (§5.5 step 4).
	if pattern != "" {
		if only, ok := singleSubstringMatch(branches, pattern); ok {
			_, werr := fmt.Fprintln(os.Stdout, only)
			return errs.PipeExit(werr, "stdout")
		}
	}

	return branchInteractive(branches, current, pattern)
}

// branchInteractive feeds "{marker}\t{branch}" records to fzf (marker "*" for the
// current branch, " " otherwise) and prints the selected branch name — field 2
// (§5.5 steps 5–6).
func branchInteractive(branches []string, current, pattern string) error {
	child, err := branchFzf(pattern)
	if err != nil {
		return err
	}

	var (
		selection string
		selected  bool
	)
	for _, record := range branchDisplayRecords(branches, current) {
		sel, werr := child.Write(record)
		if werr != nil {
			return werr
		}
		if sel != nil {
			selection, selected = *sel, true
			break
		}
	}
	if !selected {
		sel, werr := child.Wait()
		if werr != nil {
			return werr
		}
		selection = sel
	}

	// Print field 2 (the clean branch name) from "{marker}\t{branch}".
	fields := strings.SplitN(strings.TrimRight(selection, "\n"), "\t", 2)
	if len(fields) < 2 {
		return fmt.Errorf("could not read selection from fzf")
	}
	_, werr := fmt.Fprintln(os.Stdout, fields[1])
	return errs.PipeExit(werr, "stdout")
}

// branchFzf builds the branch-picker fzf child. The marker is field 1 and the
// branch is field 2; --nth=2 (from the base args) matches the branch only, so
// the marker is visible but not searchable. PATTERN seeds the initial query.
func branchFzf(pattern string) (*fzf.Child, error) {
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
	if pattern != "" {
		f.Args("--query=" + pattern)
	}
	return f.Spawn()
}

// branchDisplayRecords builds the "{marker}\t{branch}" fzf records: marker "*"
// for the current branch, " " otherwise, in the given (current-first) order
// (§5.5 step 5).
func branchDisplayRecords(branches []string, current string) []string {
	out := make([]string, len(branches))
	for i, b := range branches {
		marker := " "
		if b == current {
			marker = "*"
		}
		out[i] = marker + "\t" + b
	}
	return out
}

// moveToFront returns branches with target moved to the front (preserving the
// relative order of the rest). If target is absent, branches is returned as-is.
func moveToFront(branches []string, target string) []string {
	out := make([]string, 0, len(branches))
	out = append(out, target)
	found := false
	for _, b := range branches {
		if b == target {
			found = true
			continue
		}
		out = append(out, b)
	}
	if !found {
		return branches
	}
	return out
}

// singleSubstringMatch returns the sole branch that case-sensitively contains
// pattern, and true, iff exactly one branch matches (§5.5 step 4).
func singleSubstringMatch(branches []string, pattern string) (string, bool) {
	var match string
	count := 0
	for _, b := range branches {
		if strings.Contains(b, pattern) {
			match = b
			count++
		}
	}
	return match, count == 1
}
