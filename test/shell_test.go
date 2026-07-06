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
