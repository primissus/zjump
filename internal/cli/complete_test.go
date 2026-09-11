package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// completionGroups buckets candidates by group, preserving emission order.
func completionGroups(cands []completionCandidate) map[string][]string {
	groups := map[string][]string{}
	for _, c := range cands {
		groups[c.group] = append(groups[c.group], c.word)
	}
	return groups
}

// TestBuildCompletionsSectionsAndOrder locks the three sections and their
// priorities: current directory first, then aliases, then indexed dirs in the
// match section; fuzzy non-prefix matches in "completion"; remaining
// highest-frecency dirs in "indexed".
func TestBuildCompletionsSectionsAndOrder(t *testing.T) {
	setupDataDir(t)
	root := t.TempDir()
	cwd := filepath.Join(root, "cwd")
	if err := os.MkdirAll(filepath.Join(cwd, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"apple", "application", "map", "snap", "banana"} {
		dir := filepath.Join(root, "db", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := runAdd([]string{dir}); err != nil {
			t.Fatal(err)
		}
	}
	if err := runAlias([]string{"app", filepath.Join(root, "db", "apple")}); err != nil {
		t.Fatal(err)
	}

	groups := completionGroups(buildCompletions(cwd, "ap", 10))

	if got, want := strings.Join(groups[groupMatch], ","), "apps,app,apple,application"; got != want {
		t.Errorf("match = %q, want %q", got, want)
	}
	// "map" (gap 1) is closer than "snap" (gap 2).
	if got, want := strings.Join(groups[groupCompletion], ","), "map,snap"; got != want {
		t.Errorf("completion = %q, want %q", got, want)
	}
	if got, want := strings.Join(groups[groupIndexed], ","), "banana"; got != want {
		t.Errorf("indexed = %q, want %q", got, want)
	}
}

// TestBuildCompletionsTopLimit caps the non-match sections at --top.
func TestBuildCompletionsTopLimit(t *testing.T) {
	setupDataDir(t)
	root := t.TempDir()
	cwd := t.TempDir()
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := runAdd([]string{dir}); err != nil {
			t.Fatal(err)
		}
	}

	// "zz" matches nothing as a prefix or subsequence, so every dir lands in the
	// indexed section, capped at 3.
	groups := completionGroups(buildCompletions(cwd, "zz", 3))
	if got := len(groups[groupIndexed]); got != 3 {
		t.Errorf("indexed len = %d, want 3", got)
	}
}

// TestBuildCompletionsHiddenDirs mirrors native completion: dotfiles are only
// offered once the typed word starts with a dot.
func TestBuildCompletionsHiddenDirs(t *testing.T) {
	setupDataDir(t)
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, "visible"), 0o755); err != nil {
		t.Fatal(err)
	}

	groups := completionGroups(buildCompletions(cwd, "v", 10))
	if got := strings.Join(groups[groupMatch], ","); got != "visible" {
		t.Errorf("match(v) = %q, want visible", got)
	}

	groups = completionGroups(buildCompletions(cwd, ".", 10))
	if got := strings.Join(groups[groupMatch], ","); !strings.Contains(got, ".hidden") {
		t.Errorf("match(.) = %q, want it to contain .hidden", got)
	}
}

// TestRunCompleteOutputFormat checks the line protocol and the empty-word
// no-op.
func TestRunCompleteOutputFormat(t *testing.T) {
	setupDataDir(t)
	root := t.TempDir()
	cwd := filepath.Join(root, "cwd")
	if err := os.MkdirAll(filepath.Join(cwd, "apple"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(old) }()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error { return runComplete([]string{"--", "ap"}) })
	if err != nil {
		t.Fatal(err)
	}
	want := "match\tapple\t" + filepath.Join(wd, "apple") + "\n"
	if out != want {
		t.Errorf("runComplete output = %q, want %q", out, want)
	}

	out, err = captureStdout(t, func() error { return runComplete([]string{"--", ""}) })
	if err != nil || out != "" {
		t.Errorf("empty word: out=%q err=%v, want empty/nil", out, err)
	}
}
