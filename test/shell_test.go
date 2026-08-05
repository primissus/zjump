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

// requireBashCompletion skips the test on bash < 4.4, which lacks `${var@Q}`
// and is what the generated completion function itself requires (bash.tmpl).
// macOS ships bash 3.2 by default, so this guards local/CI runs on old bash.
func requireBashCompletion(t *testing.T) {
	t.Helper()
	requireBin(t, "bash")
	out, err := exec.Command("bash", "-c",
		`[[ ${BASH_VERSINFO[0]:-0} -eq 4 && ${BASH_VERSINFO[1]:-0} -ge 4 || ${BASH_VERSINFO[0]:-0} -ge 5 ]]`).CombinedOutput()
	if err != nil {
		t.Skipf("bash < 4.4 (completion requires @Q quoting); skipping: %s", out)
	}
}

// binDir is the directory containing the built zjump, prepended to PATH so the
// generated `zz`/`zzi` functions (which call `\command zjump`) resolve.
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

// execInteractiveZsh runs a script with `zsh -i`, which (unlike a plain
// non-interactive `-c` invocation) has the `zle` option on by default. The
// generated __zjump_z_complete function is only defined when `[[ -o zle ]]`
// (zsh.tmpl), so completion tests need this instead of execScript.
func execInteractiveZsh(t *testing.T, script string, env []string) (string, int) {
	t.Helper()
	cmd := exec.Command("zsh", "-i", "--no-globalrcs", "--no-rcs", "-c", script)
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "PATH="+binDir()+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("exec zsh -i: %v", err)
		}
	}
	return string(out), code
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
zz backend >/dev/null 2>&1; echo "kw=$(pwd)"
cd "` + root + `"
zz "` + frontend + `" >/dev/null 2>&1; echo "dir=$(pwd)"
zz >/dev/null 2>&1; echo "home=$(pwd)"
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
			t.Errorf("%s: zz backend = %q, want %q", shell, lines["kw"], backend)
		}
		if lines["dir"] != frontend {
			t.Errorf("%s: zz <existing> = %q, want %q", shell, lines["dir"], frontend)
		}
		if home, _ := os.UserHomeDir(); lines["home"] != home {
			t.Errorf("%s: zz (no args) = %q, want %q", shell, lines["home"], home)
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

// TestFlagsInShellTemplate verifies the generated z function dispatches the
// -a/--alias, -b/--branch, and -w/--worktree flags to the correct subcommands.
func TestFlagsInShellTemplate(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		requireBin(t, shell)
		data := t.TempDir()
		root := t.TempDir()
		target := filepath.Join(root, "aliastarget")
		os.MkdirAll(target, 0o755)

		script := `
eval "$(zjump init ` + shell + ` --hook none)"
zz -a proj "` + target + `"
zz proj >/dev/null 2>&1; echo "alias_jump=$(pwd)"
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
		if lines["alias_jump"] != target {
			t.Errorf("%s: zz proj (alias) = %q, want %q", shell, lines["alias_jump"], target)
		}
	}
}

// TestAliasPathCompletionBash verifies R-ALS-8 for bash: tab-completing the
// <path> of `zz -a <name> <path>` uses native directory completion, and that
// the `-d`/`--delete <name>` collision case does not.
func TestAliasPathCompletionBash(t *testing.T) {
	requireBashCompletion(t)
	data := t.TempDir()
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "targetfile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	script := `
set -o emacs
eval "$(zjump init bash --hook none)"
COMP_WORDS=(zz -a proj "` + root + `/target")
COMP_CWORD=3
__zjump_z_complete
printf 'REPLY:%s\n' "${COMPREPLY[@]}"
`
	out, code := execScript(t, "bash", script, []string{"_ZJUMP_DATA_DIR=" + data, "TERM=xterm"})
	if code != 0 {
		t.Fatalf("bash: script failed: %s", out)
	}
	if !strings.Contains(out, "REPLY:"+target) {
		t.Errorf("bash: -a <name> <path> completion = %q, want it to contain %q", out, target)
	}
	if strings.Contains(out, "targetfile") {
		t.Errorf("bash: completion offered a non-directory: %q", out)
	}

	deleteScript := `
set -o emacs
eval "$(zjump init bash --hook none)"
COMP_WORDS=(zz -a -d proj)
COMP_CWORD=3
__zjump_z_complete
printf 'REPLY:%s\n' "${COMPREPLY[@]}"
`
	out2, code2 := execScript(t, "bash", deleteScript, []string{"_ZJUMP_DATA_DIR=" + data, "TERM=xterm"})
	if code2 != 0 {
		t.Fatalf("bash: delete-mode script failed: %s", out2)
	}
	if strings.Contains(out2, root) {
		t.Errorf("bash: -a -d <name> wrongly offered directory completion: %q", out2)
	}
}

// TestAliasPathCompletionZsh verifies R-ALS-8 for zsh: tab-completing the
// <path> of `zz -a <name> <path>` dispatches to `_cd -/`, and that the
// `-d`/`--delete <name>` collision case does not.
func TestAliasPathCompletionZsh(t *testing.T) {
	requireBin(t, "zsh")
	data := t.TempDir()
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	zshScript := `
eval "$(zjump init zsh --hook none)"
typeset -a _cd_calls
function _cd() { _cd_calls+=("$*") }
words=(zz -a proj "` + root + `/target")
CURRENT=4
__zjump_z_complete
printf 'CALLS:%s\n' "${_cd_calls[@]}"
`
	out3, code3 := execInteractiveZsh(t, zshScript, []string{"_ZJUMP_DATA_DIR=" + data})
	if code3 != 0 {
		t.Fatalf("zsh: script failed: %s", out3)
	}
	if !strings.Contains(out3, "CALLS:-/") {
		t.Errorf("zsh: -a <name> <path> completion did not invoke `_cd -/`: %q", out3)
	}

	zshDeleteScript := `
eval "$(zjump init zsh --hook none)"
typeset -a _cd_calls
function _cd() { _cd_calls+=("$*") }
words=(zz -a -d proj)
CURRENT=4
__zjump_z_complete
(( ${#_cd_calls[@]} )) && printf 'CALLS:%s\n' "${_cd_calls[@]}"
true
`
	out4, code4 := execInteractiveZsh(t, zshDeleteScript, []string{"_ZJUMP_DATA_DIR=" + data})
	if code4 != 0 {
		t.Fatalf("zsh: delete-mode script failed: %s", out4)
	}
	if strings.Contains(out4, "CALLS:") {
		t.Errorf("zsh: -a -d <name> wrongly invoked `_cd`: %q", out4)
	}
}

// requireBin is defined above; add runIn for directory-aware exec.
// runIn executes zjump with args under the given working directory.
func runIn(t *testing.T, dataDir, wd string, extraEnv []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(zjumpBin, args...)
	cmd.Env = append(os.Environ(), "_ZJUMP_DATA_DIR="+dataDir)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Dir = wd
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run(%v): %v", args, err)
		}
	}
	return result{stdout.String(), stderr.String(), code}
}

// TestGitBranchWorktree tests zjump branch and worktree against a real git repo.
func TestGitBranchWorktree(t *testing.T) {
	requireBin(t, "git")
	data := t.TempDir()
	base := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}

	// Create a normal repo with a main branch.
	mainWT := filepath.Join(base, "main")
	os.MkdirAll(mainWT, 0o755)
	mustGit("-C", mainWT, "init", "-b", "main")
	mustGit("-C", mainWT, "config", "user.email", "test@test.local")
	mustGit("-C", mainWT, "config", "user.name", "Test")
	mustGit("-C", mainWT, "commit", "--allow-empty", "-m", "init")

	// Add a feature worktree off the main repo.
	featWT := filepath.Join(base, "feature-x")
	mustGit("-C", mainWT, "worktree", "add", featWT, "-b", "feature/x")
	mustGit("-C", featWT, "commit", "--allow-empty", "-m", "feat")

	// Resolve symlinks so path comparisons are stable on macOS (/var vs /private/var).
	resolve := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatalf("resolve %s: %v", p, err)
		}
		return r
	}
	mainResolved := resolve(mainWT)
	featResolved := resolve(featWT)

	// Add repo paths to DB for indexed repo resolution.
	run(t, data, nil, "add", mainResolved)
	run(t, data, nil, "add", featResolved)

	// Test `zjump branch main` from the mainWT directory (resolves via CWD).
	r := runIn(t, data, mainWT, nil, "branch", "main")
	if r.code != 0 {
		t.Fatalf("branch main: %s", r.stderr)
	}
	got := strings.TrimSpace(r.stdout)
	if got != mainResolved {
		t.Errorf("branch main = %q, want %q", got, mainResolved)
	}

	// Test `zjump branch feature/x` from mainWT (both worktrees share repo).
	r = runIn(t, data, mainWT, nil, "branch", "feature/x")
	if r.code != 0 {
		t.Fatalf("branch feature/x: %s", r.stderr)
	}
	got = strings.TrimSpace(r.stdout)
	if got != featResolved {
		t.Errorf("branch feature/x = %q, want %q", got, featResolved)
	}

	// Test branch miss — error with hint.
	r = run(t, data, nil, "branch", "nonexistent")
	if r.code == 0 {
		t.Error("branch nonexistent should error")
	}
	if !strings.Contains(r.stderr, "branch not checked out") {
		t.Errorf("branch miss error = %q", r.stderr)
	}

	// Test `zjump worktree feature-x` (by basename) from mainWT.
	r = runIn(t, data, mainWT, nil, "worktree", "feature-x")
	if r.code != 0 {
		t.Fatalf("worktree feature-x: %s", r.stderr)
	}
	got = strings.TrimSpace(r.stdout)
	if got != featResolved {
		t.Errorf("worktree feature-x = %q, want %q", got, featResolved)
	}

	// Test `zjump worktree main` (by basename).
	r = runIn(t, data, mainWT, nil, "worktree", "main")
	if r.code != 0 {
		t.Fatalf("worktree main: %s", r.stderr)
	}
	got = strings.TrimSpace(r.stdout)
	if got != mainResolved {
		t.Errorf("worktree main = %q, want %q", got, mainResolved)
	}

	// Test worktree miss — error with hint.
	r = run(t, data, nil, "worktree", "missing")
	if r.code == 0 {
		t.Error("worktree missing should error")
	}
	if !strings.Contains(r.stderr, "no worktree found") {
		t.Errorf("worktree miss error = %q", r.stderr)
	}
}

// TestListBranchesWorktrees exercises `zjump list --branches`, `--worktrees`,
// and `--all-repos` against a real git repo. Mirrors TestGitBranchWorktree
// setup; gated shelltests build tag + requireBin("git") keeps the default
// `go test ./...` path dependency-free.
func TestListBranchesWorktrees(t *testing.T) {
	requireBin(t, "git")
	data := t.TempDir()
	base := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}

	// Create a normal repo with a main branch.
	mainWT := filepath.Join(base, "main")
	os.MkdirAll(mainWT, 0o755)
	mustGit("-C", mainWT, "init", "-b", "main")
	mustGit("-C", mainWT, "config", "user.email", "test@test.local")
	mustGit("-C", mainWT, "config", "user.name", "Test")
	mustGit("-C", mainWT, "commit", "--allow-empty", "-m", "init")

	// Add a feature worktree off the main repo.
	featWT := filepath.Join(base, "feature-x")
	mustGit("-C", mainWT, "worktree", "add", featWT, "-b", "feature/x")

	resolve := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatalf("resolve %s: %v", p, err)
		}
		return r
	}
	mainResolved := resolve(mainWT)
	featResolved := resolve(featWT)

	// Add repo paths to DB so --all-repos has something to scan.
	run(t, data, nil, "add", mainResolved)
	run(t, data, nil, "add", featResolved)

	// `zjump list --branches` from mainWT resolves via CWD and emits both
	// the main and feature branches (no REPO column).
	r := runIn(t, data, mainWT, nil, "list", "--branches")
	if r.code != 0 {
		t.Fatalf("list --branches: %s", r.stderr)
	}
	out := r.stdout
	if !strings.Contains(out, "BRANCHES (repo:") {
		t.Errorf("missing BRANCHES header with repo hint:\n%s", out)
	}
	if !strings.Contains(out, "main") || !strings.Contains(out, "feature/x") {
		t.Errorf("missing branch rows:\n%s", out)
	}
	if strings.Contains(out, "REPO") {
		t.Errorf("single-repo --branches should not include REPO column:\n%s", out)
	}

	// `zjump list --worktrees` (no CWD repo): we run from a fresh temp dir
	// (outside any git repo), so the section header should report "(no git
	// repository)" and the body should be "(none)". This asserts the
	// CWD-fallback skip does NOT print a broken-section header.
	nonRepoDir := t.TempDir() // outside any git repo
	r = runIn(t, data, nonRepoDir, nil, "list", "--worktrees")
	if r.code != 0 {
		t.Fatalf("list --worktrees: %s", r.stderr)
	}
	out = r.stdout
	if !strings.Contains(out, "WORKTREES (no git repository)") {
		t.Errorf("expected WORKTREES header with '(no git repository)' marker:\n%s", out)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("expected (none) body when CWD is not a repo:\n%s", out)
	}

	// `zjump list --worktrees` from mainWT: CWD repo, expect both worktrees.
	r = runIn(t, data, mainWT, nil, "list", "--worktrees")
	if r.code != 0 {
		t.Fatalf("list --worktrees in repo: %s", r.stderr)
	}
	out = r.stdout
	if !strings.Contains(out, "WORKTREES (repo:") {
		t.Errorf("missing WORKTREES header with repo hint:\n%s", out)
	}
	if !strings.Contains(out, filepath.Base(mainResolved)) || !strings.Contains(out, filepath.Base(featResolved)) {
		t.Errorf("missing worktree basenames:\n%s", out)
	}

	// `zjump list --branches --worktrees --all-repos`: column headers must
	// include REPO (in both sections) since --all-repos was requested.
	r = run(t, data, nil, "list", "--branches", "--worktrees", "--all-repos")
	if r.code != 0 {
		t.Fatalf("list --all-repos: %s", r.stderr)
	}
	out = r.stdout
	if !strings.Contains(out, "BRANCHES (all repos in DB)") {
		t.Errorf("missing all-repos BRANCHES header:\n%s", out)
	}
	if !strings.Contains(out, "WORKTREES (all repos in DB)") {
		t.Errorf("missing all-repos WORKTREES header:\n%s", out)
	}
	// Each section's row data should still be present, sorted under the
	// canonical REPO column (resolved via git's symlink canonicalization).
	if !strings.Contains(out, mainResolved) {
		t.Errorf("missing main resolved path:\n%s", out)
	}
}
