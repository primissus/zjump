package cli

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/primissus/zjump/internal/db"
)

// containsAll asserts every needle appears in haystack (failing with a list
// of missing needles). Used everywhere here so tabwriter's padding can mutate
// the precise `\t` separators without breaking test expectations.
func containsAll(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	var missing []string
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Errorf("missing substrings %v in:\n%s", missing, haystack)
	}
}

// containsNone asserts none of the needles appears in haystack.
func containsNone(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			t.Errorf("unexpected substring %q in:\n%s", n, haystack)
		}
	}
}

// TestPrintListText_DirectoriesOnly checks the default bare `zjump list` body —
// only DIRECTORIES, no score column, no headers beyond the section title.
func TestPrintListText_DirectoriesOnly(t *testing.T) {
	r := &listReport{
		Directories: []listDir{
			{Path: "/a", Score: 4.0},
			{Path: "/b", Score: 2.0},
		},
	}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	out := buf.String()
	containsAll(t, out, "DIRECTORIES\n", strings.Repeat("-", len("DIRECTORIES"))+"\n", "PATH", "/a", "/b")
	containsNone(t, out, "SCORE", "ALIASES", "BRANCHES", "WORKTREES")
}

// TestPrintListText_DirectoriesScore asserts that the optional SCORE column
// is emitted when score=true. Mirrors the fixed %6.1f formatting of
// Dir.DisplayScore.
func TestPrintListText_DirectoriesScore(t *testing.T) {
	r := &listReport{
		Directories: []listDir{{Path: "/x", Score: 4.0}},
	}
	var buf bytes.Buffer
	if err := printListText(&buf, r, true); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "   4.0") {
		t.Errorf("missing 6.1f-formatted score in:\n%s", out)
	}
	containsAll(t, out, "SCORE", "PATH", "/x")
}

// TestPrintListText_DirectoriesEmpty asserts that an empty DIRECTORIES
// section still prints its header + a "(none)" row.
func TestPrintListText_DirectoriesEmpty(t *testing.T) {
	r := &listReport{}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	containsAll(t, buf.String(), "DIRECTORIES", "PATH", "(none)")
}

// TestPrintListText_NoDirs asserts that --no-dirs suppresses the DIRECTORIES
// section entirely (no header, no "(none)", no preceding separator).
func TestPrintListText_NoDirs(t *testing.T) {
	r := &listReport{
		NoDirs: true,
		Directories: []listDir{
			{Path: "/a", Score: 4.0},
		},
		ShowAlias: true,
		Aliases:   []listAlias{{Name: "k", Path: "/v"}},
	}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	out := buf.String()
	containsNone(t, out, "DIRECTORIES")
	// PATH alone is shared with the ALIASES section's column header; what we
	// actually care about is that no DIRECTORIES section's "PATH" lingers —
	// since the whole DIRECTORIES header is gone, the leak would manifest as
	// the bare "DIRECTORIES" word. Check ALIASES specifically.
	containsAll(t, out, "ALIASES", "NAME", "k")
	// And both ALIASES section path values should still appear.
	if !strings.Contains(out, "/v") {
		t.Errorf("missing alias target /v in:\n%s", out)
	}
}

// TestPrintListText_AliasesEmpty asserts that an explicitly-requested empty
// ALIASES section still prints its header and "(none)" row — distinguishes
// "suppressed" from "asked for, but none configured".
func TestPrintListText_AliasesEmpty(t *testing.T) {
	r := &listReport{ShowAlias: true}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	containsAll(t, buf.String(), "ALIASES", strings.Repeat("-", len("ALIASES")), "NAME", "PATH", "(none)")
}

// TestPrintListText_SectionSeparator asserts that consecutive sections are
// separated by exactly one blank line (no tabwriter padding issues since
// the blank-line separator is just "\n").
func TestPrintListText_SectionSeparator(t *testing.T) {
	r := &listReport{
		Directories: []listDir{{Path: "/a"}},
		ShowAlias:   true,
		Aliases:     []listAlias{{Name: "k", Path: "/v"}},
	}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	if !strings.Contains(buf.String(), "/a\n\nALIASES") {
		t.Errorf("expected blank-line separator between sections in:\n%s", buf.String())
	}
}

// TestPrintListText_BranchesMissingRepo asserts the branch section's header is
// "BRANCHES (no git repository)" when RepoHint is empty.
func TestPrintListText_BranchesMissingRepo(t *testing.T) {
	r := &listReport{ShowBranch: true}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	containsAll(t, buf.String(), "BRANCHES (no git repository)", "BRANCH", "PATH")
}

// TestPrintListText_BranchesSingleRepo asserts the branch section's header
// carries the repo hint and the columns are BRANCH/PATH (no REPO column).
func TestPrintListText_BranchesSingleRepo(t *testing.T) {
	r := &listReport{
		ShowBranch: true,
		RepoHint:   "/home/u/repo",
		Branches:   []listBranch{{Branch: "main", Path: "/home/u/repo"}},
	}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	out := buf.String()
	containsAll(t, out, "BRANCHES (repo: /home/u/repo)", "main", "/home/u/repo")
	// Single-repo mode deliberately omits the REPO column. The whole-word
	// "REPO" header column label must not appear.
	if strings.Contains(out, "REPO") {
		t.Errorf("REPO column leaked into single-repo mode:\n%s", out)
	}
}

// TestPrintListText_BranchesAllRepos asserts the BRANCHES section gains a REPO
// leading column in --all-repos mode.
func TestPrintListText_BranchesAllRepos(t *testing.T) {
	r := &listReport{
		ShowBranch: true,
		AllRepos:   true,
		Branches:   []listBranch{{Repo: "/r1", Branch: "main", Path: "/r1"}},
	}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	out := buf.String()
	containsAll(t, out, "BRANCHES (all repos in DB)", "REPO", "BRANCH", "PATH", "/r1", "main")
}

// TestPrintListText_WorktreesAllRepos asserts the WORKTREES section gains a
// REPO leading column in --all-repos mode (4 columns total).
func TestPrintListText_WorktreesAllRepos(t *testing.T) {
	r := &listReport{
		ShowWT:   true,
		AllRepos: true,
		Worktrees: []listWorktree{
			{Repo: "/r1", Basename: "r1", Branch: "main", Path: "/r1"},
		},
	}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	out := buf.String()
	containsAll(t, out, "WORKTREES (all repos in DB)", "REPO", "BASENAME", "BRANCH", "PATH", "/r1", "main")
}

// TestPrintListText_WorktreesSingleRepoDetached asserts detached worktrees
// render with "(detached)" as their branch label so the column stays
// populated.
func TestPrintListText_WorktreesSingleRepoDetached(t *testing.T) {
	r := &listReport{
		ShowWT:   true,
		RepoHint: "/r",
		Worktrees: []listWorktree{
			{Basename: "main", Branch: "(detached)", Detached: true, Path: "/r"},
		},
	}
	var buf bytes.Buffer
	if err := printListText(&buf, r, false); err != nil {
		t.Fatalf("printListText: %v", err)
	}
	containsAll(t, buf.String(), "WORKTREES (repo: /r)", "(detached)")
}

// TestPrintListJSON_MinimalDir asserts that with no section overrides the
// JSON is exactly {"directories": [...]} and other section keys are absent.
func TestPrintListJSON_MinimalDir(t *testing.T) {
	r := &listReport{
		Directories: []listDir{{Path: "/a", Rank: 1, LastAccessed: 100, Score: 4}},
	}
	var buf bytes.Buffer
	if err := printListJSON(&buf, r); err != nil {
		t.Fatalf("printListJSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	containsNone(t, buf.String(), `"aliases"`, `"branches"`, `"worktrees"`)
	dirs, ok := got["directories"].([]any)
	if !ok || len(dirs) != 1 {
		t.Errorf("directories = %v, want 1-element array", got["directories"])
	}
}

// TestPrintListJSON_AllSections asserts all section keys are present when
// requested, with nil slices serialized as empty arrays (not null).
func TestPrintListJSON_AllSections(t *testing.T) {
	r := &listReport{
		Directories: []listDir{{Path: "/a", Rank: 1, LastAccessed: 100, Score: 4}},
		ShowAlias:   true,
		Aliases:     nil,
		ShowBranch:  true,
		Branches:    nil,
		ShowWT:      true,
		Worktrees:   nil,
	}
	var buf bytes.Buffer
	if err := printListJSON(&buf, r); err != nil {
		t.Fatalf("printListJSON: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"directories", "aliases", "branches", "worktrees"} {
		raw, ok := got[key]
		if !ok {
			t.Errorf("missing key %q in:\n%s", key, buf.String())
			continue
		}
		// A requested-but-nil section must serialize as `[]`, not `null`,
		// so consumers can distinguish "asked for, none found" from
		// "section not requested" (in which case the key is omitted).
		s := strings.TrimSpace(string(raw))
		if s == "null" {
			t.Errorf("section %q = null; want [] (or populated array)", key)
		}
	}
}

// TestPrintListJSON_OmitEmptyUnrequested asserts that unrequested sections are
// genuinely absent from the JSON object — only `directories` is always
// present.
func TestPrintListJSON_OmitEmptyUnrequested(t *testing.T) {
	r := &listReport{
		Directories: []listDir{},
	}
	var buf bytes.Buffer
	if err := printListJSON(&buf, r); err != nil {
		t.Fatalf("printListJSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"directories"`) {
		t.Errorf("directories must always serialize:\n%s", buf.String())
	}
	containsNone(t, buf.String(), `"aliases"`, `"branches"`, `"worktrees"`)
}

// TestPrintListJSON_NoDirsStillSerializes asserts that --no-dirs still emits
// `"directories": []` for schema stability (text mode suppresses, JSON
// includes it as an empty array).
func TestPrintListJSON_NoDirsStillSerializes(t *testing.T) {
	r := &listReport{
		NoDirs:    true,
		ShowAlias: true,
		Aliases:   []listAlias{{Name: "k", Path: "/v"}},
	}
	var buf bytes.Buffer
	if err := printListJSON(&buf, r); err != nil {
		t.Fatalf("printListJSON: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"directories": []`) && !strings.Contains(out, `"directories":[]`) {
		t.Errorf("--no-dirs should still serialize directories as [] in:\n%s", out)
	}
	containsAll(t, out, `"aliases"`, `"k"`, `"/v"`)
}

// TestClampScore asserts clampScore matches Dir.DisplayScore's [0, 9999]
// clamping behavior (internal/db/dir.go:67).
func TestClampScore(t *testing.T) {
	tests := []struct {
		in, want db.Rank
	}{
		{-1.0, 0.0},
		{0.0, 0.0},
		{4.0, 4.0},
		{9999.0, 9999.0},
		{10000.0, 9999.0},
		{1e12, 9999.0},
	}
	for _, tc := range tests {
		if got := clampScore(tc.in); got != tc.want {
			t.Errorf("clampScore(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestCollectAllReposRows_Dedup covers the canonical-repo dedup so a single
// repo reached through multiple tracked worktree paths only emits one set of
// rows. Skips via exec.LookPath if git isn't available.
func TestCollectAllReposRows_Dedup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed; skipping")
	}
	repoDir := t.TempDir()
	if out, err := runGitIn(repoDir, "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := runGitIn(repoDir, "config", "user.email", "t@e.st"); err != nil {
		t.Fatalf("git config user.email: %v\n%s", err, out)
	}
	if out, err := runGitIn(repoDir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("git config user.name: %v\n%s", err, out)
	}
	if out, err := runGitIn(repoDir, "commit", "--allow-empty", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	// macOS symlinks /tmp → /private/tmp; git resolves these to the canonical
	// path when emitting worktree porcelain output. Canonicalize before
	// comparing against `git worktree list --porcelain` rows.
	canonical, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	wt2 := filepath.Join(filepath.Dir(repoDir), "wt2-"+filepath.Base(repoDir))
	if out, err := runGitIn(repoDir, "worktree", "add", wt2, "-b", "feature"); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	// Both tracked dirs point INSIDE the same repo — dedup keys on the
	// canonical repo path (first worktree = main checkout) so the rows of the
	// one-and-only repo are emitted exactly once.
	dirs := []listDir{
		{Path: repoDir},
		{Path: wt2},
	}
	branches, worktrees := collectAllReposRows(dirs, true, true)

	if len(branches) != 2 {
		t.Errorf("dedup'd branches = %d, want 2 (main + feature): %+v", len(branches), branches)
	}
	if len(worktrees) != 2 {
		t.Errorf("dedup'd worktrees = %d, want 2: %+v", len(worktrees), worktrees)
	}
	for _, b := range branches {
		if b.Repo != canonical {
			t.Errorf("branch row Repo = %q, want %q", b.Repo, canonical)
		}
	}
}

// runGitIn shells out to `git -C dir args...` and returns combined output
// (or the wrap error). Used only by git-enabled list unit tests.
func runGitIn(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}
