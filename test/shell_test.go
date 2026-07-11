//go:build shelltests

// These tests shell out to real bash, zsh, and fzf. They are gated behind the
// `shelltests` build tag so the default `go test ./...` stays dependency-free
// (A-8). Run with: go test -tags shelltests ./test/
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireBin(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed; skipping", name)
	}
}

// binDir is the directory containing the built zjump, prepended to PATH so the
// generated `z`/`zi` functions (which call `\command zjump`) resolve.
func binDir() string { return filepath.Dir(zjumpBin) }

// renderInit runs `zjump init <shell> <flags...>` and returns the script.
func renderInit(t *testing.T, data, shell string, env []string, flags ...string) string {
	t.Helper()
	args := append([]string{"init", shell}, flags...)
	r := run(t, data, env, args...)
	if r.code != 0 {
		t.Fatalf("init %s %v failed: %s", shell, flags, r.stderr)
	}
	return r.stdout
}

// execScript runs a script in the given shell with strict flags, returning
// combined output and exit code.
func execScript(t *testing.T, shell, script string, env []string) (string, int) {
	t.Helper()
	var args []string
	switch shell {
	case "bash":
		args = []string{"--noprofile", "--norc", "-u", "-o", "pipefail", "-c", script}
	case "zsh":
		args = []string{"-u", "-o", "pipefail", "--no-globalrcs", "--no-rcs", "-c", script}
	}
	cmd := exec.Command(shell, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "PATH="+binDir()+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("exec %s: %v", shell, err)
		}
	}
	return string(out), code
}

// runInDir runs zjump with args from working directory dir (needed by `branch`,
// which inspects the current repository via cwd).
func runInDir(t *testing.T, dir, dataDir string, extraEnv []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(zjumpBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "_ZJUMP_DATA_DIR="+dataDir)
	cmd.Env = append(cmd.Env, extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("runInDir(%v): %v", args, err)
		}
	}
	return result{stdout.String(), stderr.String(), code}
}

// git runs a git command, failing the test on error. Global/system config is
// neutralized so the host environment can't interfere.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitInitRepo creates a repository at dir with one commit on branch `main`.
func gitInitRepo(t *testing.T, dir string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	git(t, dir, "-c", "init.defaultBranch=main", "init")
	os.WriteFile(filepath.Join(dir, "README"), []byte("hi\n"), 0o644)
	git(t, dir, "add", "README")
	git(t, dir, "commit", "-m", "init")
}

// TestWorktreeEndToEnd indexes a real repo with a linked worktree and checks the
// `query --type worktree` output in default, --list, and -i (fzf field-2) modes
// (R2-WT-2, §9 Phase 5).
func TestWorktreeEndToEnd(t *testing.T) {
	requireBin(t, "git")
	data := t.TempDir()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	feature := filepath.Join(root, "wt-feature")

	gitInitRepo(t, repo)
	git(t, repo, "worktree", "add", feature, "-b", "feature")

	// Index the repo (auto-typed KindRepo).
	if r := run(t, data, nil, "add", repo); r.code != 0 {
		t.Fatalf("add repo failed: %s", r.stderr)
	}

	// --list: both the main and linked worktrees appear.
	list := run(t, data, nil, "query", "--type", "worktree", "--list").stdout
	if !strings.Contains(list, feature) {
		t.Errorf("worktree --list missing linked worktree %q:\n%s", feature, list)
	}
	if len(splitLines(list)) < 2 {
		t.Errorf("expected >=2 worktrees, got:\n%s", list)
	}

	// default: prints a single worktree path (best repo's first worktree).
	first := strings.TrimSpace(run(t, data, nil, "query", "--type", "worktree").stdout)
	if first == "" {
		t.Error("default worktree query printed nothing")
	}

	// -i via fzf filter mode: field-2 (path) extraction from the 3-field record.
	if _, err := exec.LookPath("fzf"); err == nil {
		r := run(t, data, []string{"FZF_DEFAULT_OPTS=--filter=wt-feature"}, "query", "--type", "worktree", "-i")
		if got := strings.TrimSpace(r.stdout); got != feature {
			t.Errorf("worktree -i field-2 = %q, want %q (stderr=%q)", got, feature, r.stderr)
		}
	}
}

// TestBranchEndToEnd covers `zjump branch`: current-first order + marker, the
// single-match fast path (no fzf), silent exit 130, and the two error cases
// (R2-BR-1, §9 Phase 5).
func TestBranchEndToEnd(t *testing.T) {
	requireBin(t, "git")
	data := t.TempDir()
	repo := t.TempDir()
	gitInitRepo(t, repo)
	git(t, repo, "branch", "feature")
	git(t, repo, "branch", "bugfix")

	// Fast path: a pattern matching exactly one branch prints it without fzf.
	if r := runInDir(t, repo, data, nil, "branch", "feature"); r.code != 0 || strings.TrimSpace(r.stdout) != "feature" {
		t.Errorf("branch feature (fast path) = %q code=%d, want 'feature'", r.stdout, r.code)
	}
	if r := runInDir(t, repo, data, nil, "branch", "bug"); strings.TrimSpace(r.stdout) != "bugfix" {
		t.Errorf("branch bug (fast path) = %q, want 'bugfix'", r.stdout)
	}

	// Not inside a repository.
	nonRepo := t.TempDir()
	if r := runInDir(t, nonRepo, data, nil, "branch"); r.code == 0 || !strings.Contains(r.stderr, "not inside a git repository") {
		t.Errorf("branch outside repo: code=%d stderr=%q", r.code, r.stderr)
	}

	// No branches (unborn HEAD).
	empty := filepath.Join(t.TempDir(), "empty")
	os.MkdirAll(empty, 0o755)
	git(t, empty, "-c", "init.defaultBranch=main", "init")
	if r := runInDir(t, empty, data, nil, "branch"); r.code == 0 || !strings.Contains(r.stderr, "no branches found") {
		t.Errorf("branch with no commits: code=%d stderr=%q", r.code, r.stderr)
	}

	if _, err := exec.LookPath("fzf"); err == nil {
		// Interactive selection: a single-match filter drives the fzf path and
		// exercises field-2 (clean branch name) extraction from "{marker}\t{branch}".
		r := runInDir(t, repo, data, []string{"FZF_DEFAULT_OPTS=--filter=feature"}, "branch")
		if got := strings.TrimSpace(r.stdout); got != "feature" {
			t.Errorf("branch -i (filter) = %q, want 'feature' (stderr=%q)", got, r.stderr)
		}
	}
	// Current-first ordering + marker are covered deterministically by the unit
	// tests (TestBranchMoveToFrontAndMarkers); silent exit 130 is covered by the
	// shared fzf classifyExit unit test — neither is observable through fzf's
	// non-interactive --filter mode, which is the only headless driver available.
}

// splitLines returns the non-empty lines of s.
func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestInitExecutesAllCombos renders and executes every init option combination
// (2 cmd × 3 hook × 2 echo × 2 resolve) in real bash and zsh, asserting each
// sources cleanly with no output (mirrors zoxide's gated template suite).
func TestInitExecutesAllCombos(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		requireBin(t, shell)
		for _, cmdFlag := range [][]string{{}, {"--no-cmd"}} {
			for _, hook := range []string{"none", "prompt", "pwd"} {
				for _, echo := range []string{"0", "1"} {
					for _, sym := range []string{"0", "1"} {
						data := t.TempDir()
						env := []string{"_ZJUMP_ECHO=" + echo, "_ZJUMP_RESOLVE_SYMLINKS=" + sym}
						flags := append([]string{"--hook", hook}, cmdFlag...)
						src := renderInit(t, data, shell, env, flags...)
						out, code := execScript(t, shell, src, env)
						if code != 0 || strings.TrimSpace(out) != "" {
							t.Errorf("%s cmd=%v hook=%s echo=%s sym=%s: code=%d out=%q",
								shell, cmdFlag, hook, echo, sym, code, out)
						}
					}
				}
			}
		}
	}
}

// TestZJump verifies the full A-1 flow: eval init, seed the DB, `z <kw>` lands in
// the right directory, and the special dispatch cases behave.
func TestZJump(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		requireBin(t, shell)
		data := t.TempDir()
		root := t.TempDir()
		backend := filepath.Join(root, "alpha", "backend")
		frontend := filepath.Join(root, "beta", "frontend")
		os.MkdirAll(backend, 0o755)
		os.MkdirAll(frontend, 0o755)
		run(t, data, nil, "add", backend)
		run(t, data, nil, "add", backend)
		run(t, data, nil, "add", frontend)

		script := `
eval "$(zjump init ` + shell + ` --hook prompt)"
z backend >/dev/null 2>&1; echo "kw=$(pwd)"
cd "` + root + `"
z "` + frontend + `" >/dev/null 2>&1; echo "dir=$(pwd)"
z >/dev/null 2>&1; echo "home=$(pwd)"
`
		out, code := execScript(t, shell, script, []string{"_ZJUMP_DATA_DIR=" + data})
		if code != 0 {
			t.Fatalf("%s: script failed: %s", shell, out)
		}
		lines := map[string]string{}
		for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
			if k, v, ok := strings.Cut(ln, "="); ok {
				lines[k] = v
			}
		}
		if lines["kw"] != backend {
			t.Errorf("%s: z backend = %q, want %q", shell, lines["kw"], backend)
		}
		if lines["dir"] != frontend {
			t.Errorf("%s: z <existing> = %q, want %q", shell, lines["dir"], frontend)
		}
		if home, _ := os.UserHomeDir(); lines["home"] != home {
			t.Errorf("%s: z (no args) = %q, want %q", shell, lines["home"], home)
		}
	}
}

// TestHookTracks verifies the tracking hook actually calls `zjump add` when the
// directory changes, for both prompt and (bash-emulated) pwd modes.
func TestHookTracks(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		requireBin(t, shell)
		for _, hook := range []string{"prompt", "pwd"} {
			data := t.TempDir()
			target := filepath.Join(t.TempDir(), "tracked")
			os.MkdirAll(target, 0o755)

			script := `
eval "$(zjump init ` + shell + ` --hook ` + hook + `)"
cd "` + target + `"
__zjump_hook
`
			_, code := execScript(t, shell, script, []string{"_ZJUMP_DATA_DIR=" + data})
			if code != 0 {
				t.Fatalf("%s/%s: hook script failed", shell, hook)
			}
			list := run(t, data, nil, "query", "--list", "--all").stdout
			if !strings.Contains(list, target) {
				t.Errorf("%s/%s: hook did not track %s:\n%s", shell, hook, target, list)
			}
		}
	}
}

// TestQueryInteractiveFilter drives `query -i` against real fzf in filter mode
// (headless): a single-match filter yields that path; a no-match filter maps to
// "no match found" (exit 1). Validates the streaming record protocol + selection
// strip + exit-code mapping against a live fzf (R-FZF-2/3/5).
func TestQueryInteractiveFilter(t *testing.T) {
	requireBin(t, "fzf")
	data := t.TempDir()
	root := t.TempDir()
	backend := filepath.Join(root, "alpha", "backend")
	os.MkdirAll(backend, 0o755)
	run(t, data, nil, "add", backend)

	// Single match -> path returned, 7-char score prefix stripped.
	r := run(t, data, []string{"_ZJUMP_FZF_OPTS=--filter=backend"}, "query", "-i")
	if r.code != 0 || strings.TrimSpace(r.stdout) != backend {
		t.Errorf("query -i (filter) = %q code=%d, want %q", r.stdout, r.code, backend)
	}

	// With --score the full scored line is kept.
	r = run(t, data, []string{"_ZJUMP_FZF_OPTS=--filter=backend"}, "query", "-i", "--score")
	if !strings.Contains(r.stdout, backend) || !strings.Contains(r.stdout, "\t") {
		t.Errorf("query -i --score = %q, want scored line with tab", r.stdout)
	}

	// No match -> fzf exit 1 -> "no match found".
	r = run(t, data, []string{"_ZJUMP_FZF_OPTS=--filter=zzznope"}, "query", "-i")
	if r.code == 0 || !strings.Contains(r.stderr, "no match found") {
		t.Errorf("query -i no-match: code=%d stderr=%q", r.code, r.stderr)
	}
}
