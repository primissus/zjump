package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/primissus/zjump/internal/db"
)

// TestRunAliasPick_NoAliases asserts that --pick returns an error (rather than
// panicking) when the alias store is empty.
func TestRunAliasPick_NoAliases(t *testing.T) {
	t.Setenv("_ZJUMP_DATA_DIR", t.TempDir())

	err := runAlias([]string{"--pick"})
	if err == nil {
		t.Fatal("expected error for --pick with no aliases, got nil")
	}
	if !strings.Contains(err.Error(), "no aliases found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunAliasPick_SingleAlias asserts the single-offer fast path: with exactly
// one alias, the picker prints its path without invoking fzf.
func TestRunAliasPick_SingleAlias(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("_ZJUMP_DATA_DIR", dataDir)

	target := t.TempDir()
	if err := runAlias([]string{"myalias", target}); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runAlias([]string{"--pick"})
	w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("runAlias --pick error: %v", err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r)
	got := strings.TrimSpace(buf.String())
	// runAlias stores via paths.ResolvePath (lexical, no symlink resolution)
	// when _ZJUMP_RESOLVE_SYMLINKS is unset, so compare against the lexical
	// form — not filepath.EvalSymlinks which would diverge on macOS (/var).
	want := target
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRunAliasPick_RejectsCombined asserts that --pick cannot be combined with
// --delete or positional args.
func TestRunAliasPick_RejectsCombined(t *testing.T) {
	t.Setenv("_ZJUMP_DATA_DIR", t.TempDir())

	if err := runAlias([]string{"--pick", "--delete", "foo"}); err == nil {
		t.Error("expected error combining --pick with --delete, got nil")
	}
	if err := runAlias([]string{"--pick", "name", "dir"}); err == nil {
		t.Error("expected error combining --pick with positional args, got nil")
	}
}

// TestRunAliasScoreRequiresPick asserts --score is rejected unless --pick is
// also given, so it can never be silently ignored.
func TestRunAliasScoreRequiresPick(t *testing.T) {
	t.Setenv("_ZJUMP_DATA_DIR", t.TempDir())

	err := runAlias([]string{"--score"})
	if err == nil || !strings.Contains(err.Error(), "requires --pick") {
		t.Errorf("runAlias --score err = %v, want 'requires --pick'", err)
	}
}

// TestRunAliasPickScored_SingleAlias asserts the single-offer fast path still
// prints the target path when --score is set (no fzf needed).
func TestRunAliasPickScored_SingleAlias(t *testing.T) {
	setupDataDir(t)
	target := t.TempDir()
	preload(t, func(d *db.Database) {
		d.AddUpdate(target, 3.0, nowEpoch(), db.KindDir)
	})
	if err := runAlias([]string{"myalias", target}); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	out, err := captureStdout(t, func() error { return runAlias([]string{"--pick", "--score"}) })
	if err != nil {
		t.Fatalf("runAlias --pick --score error: %v", err)
	}
	if got := strings.TrimSpace(out); got != target {
		t.Errorf("got %q, want %q", got, target)
	}
}

// TestAliasTargetScores verifies the alias score is the target's clamped
// frecency and that an untracked target scores 0 (absent key).
func TestAliasTargetScores(t *testing.T) {
	setupDataDir(t)
	hot, cold := t.TempDir(), t.TempDir()
	preload(t, func(d *db.Database) {
		d.AddUpdate(hot, 5.0, nowEpoch(), db.KindRepo)
		d.AddUpdate(cold, 1.0, nowEpoch(), db.KindDir)
	})

	scores, err := aliasTargetScores()
	if err != nil {
		t.Fatalf("aliasTargetScores: %v", err)
	}
	if scores[hot] <= scores[cold] {
		t.Errorf("expected hot (%v) > cold (%v)", scores[hot], scores[cold])
	}
	if scores[cold] <= 0 {
		t.Errorf("expected cold score > 0, got %v", scores[cold])
	}
	if got := scores["/not/in/the/database"]; got != 0 {
		t.Errorf("untracked target score = %v, want 0", got)
	}
}
