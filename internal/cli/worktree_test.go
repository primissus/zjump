package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/git"
)

// withWorktreeLister swaps the package worktree enumerator for a fixture during
// the test, restoring it afterward.
func withWorktreeLister(t *testing.T, fn func(repo string) ([]git.Worktree, error)) {
	t.Helper()
	orig := worktreeLister
	worktreeLister = fn
	t.Cleanup(func() { worktreeLister = orig })
}

// splitNonEmpty splits s into trimmed, non-empty lines.
func splitNonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestWorktreeDedupAcrossRepos: a worktree path shared by two repos appears once,
// from the higher-frecency repo that streams first (§6 step 4, R2-WT-2).
func TestWorktreeDedupAcrossRepos(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.AddUpdate("/repoA", 5.0, nowEpoch(), db.KindRepo) // higher score -> first
		d.AddUpdate("/repoB", 1.0, nowEpoch(), db.KindRepo)
	})
	withWorktreeLister(t, func(repo string) ([]git.Worktree, error) {
		switch repo {
		case "/repoA":
			return []git.Worktree{{Path: "/wt/shared", Branch: "main"}, {Path: "/wt/a", Branch: "x"}}, nil
		case "/repoB":
			return []git.Worktree{{Path: "/wt/shared", Branch: "main"}, {Path: "/wt/b", Branch: "y"}}, nil
		}
		return nil, nil
	})

	out, err := runQueryCapture(t, "--type", "worktree", "-l", "--all")
	if err != nil {
		t.Fatal(err)
	}
	lines := splitNonEmpty(out)
	if len(lines) != 3 {
		t.Fatalf("want 3 worktrees (shared de-duped), got %d: %q", len(lines), out)
	}
	if strings.Count(out, "/wt/shared") != 1 {
		t.Errorf("shared worktree appeared %d times, want 1", strings.Count(out, "/wt/shared"))
	}
	if !strings.Contains(out, "/wt/a") || !strings.Contains(out, "/wt/b") {
		t.Errorf("missing per-repo worktrees: %q", out)
	}
}

// TestWorktreeRepoCapAt50: enumeration probes at most 50 repositories (§6 step 5).
func TestWorktreeRepoCapAt50(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		for i := 0; i < 60; i++ {
			d.AddUpdate(fmt.Sprintf("/repo%02d", i), 1.0, nowEpoch(), db.KindRepo)
		}
	})
	withWorktreeLister(t, func(repo string) ([]git.Worktree, error) {
		return []git.Worktree{{Path: repo + "/wt", Branch: "main"}}, nil
	})

	out, err := runQueryCapture(t, "--type", "worktree", "-l", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(splitNonEmpty(out)); n != maxWorktreeRepos {
		t.Errorf("processed %d repos, want the %d-repo cap", n, maxWorktreeRepos)
	}
}

// TestWorktreeGitFailureSkipsRepoSilently: a repo whose enumeration errors is
// skipped with no output at all (§6 step 2, R2-WT-2).
func TestWorktreeGitFailureSkipsRepoSilently(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.AddUpdate("/repoGood", 5.0, nowEpoch(), db.KindRepo)
		d.AddUpdate("/repoBad", 1.0, nowEpoch(), db.KindRepo)
	})
	withWorktreeLister(t, func(repo string) ([]git.Worktree, error) {
		if repo == "/repoBad" {
			return nil, fmt.Errorf("not a git repository")
		}
		return []git.Worktree{{Path: "/wt/good", Branch: "main"}}, nil
	})

	var out string
	stderr, err := captureStderr(t, func() error {
		var e error
		out, e = captureStdout(t, func() error {
			return runQuery([]string{"--type", "worktree", "-l", "--all"})
		})
		return e
	})
	if err != nil {
		t.Fatalf("git failure should not surface an error, got %v", err)
	}
	if stderr != "" {
		t.Errorf("a skipped repo must be silent, but stderr = %q", stderr)
	}
	if strings.TrimSpace(out) != "/wt/good" {
		t.Errorf("output = %q, want only /wt/good", out)
	}
}

// TestWorktreeFirstAndExclude: default mode prints the best worktree, and
// --exclude on the only match yields "you are already in the only match"
// (§6 step 6, step 8).
func TestWorktreeFirstAndExclude(t *testing.T) {
	setupDataDir(t)
	preload(t, func(d *db.Database) {
		d.AddUpdate("/repo", 5.0, nowEpoch(), db.KindRepo)
	})
	withWorktreeLister(t, func(repo string) ([]git.Worktree, error) {
		return []git.Worktree{{Path: "/wt/only", Branch: "main"}}, nil
	})

	out, err := runQueryCapture(t, "--type", "worktree", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "/wt/only" {
		t.Errorf("default output = %q, want /wt/only", out)
	}

	err = runQuery([]string{"--type", "worktree", "--all", "--exclude", "/wt/only"})
	if err == nil || err.Error() != "you are already in the only match" {
		t.Errorf("exclude-only err = %v, want 'you are already in the only match'", err)
	}
}
