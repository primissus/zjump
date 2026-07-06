// Package fzf wraps the external fzf binary: the NUL/tab record protocol, the
// default argument sets, the preview pane, and the exit-code mapping — including
// the silent-130 cancellation path (ARCHITECTURE.md §8, R-FZF-*). It shells out
// via os/exec; no third-party dependency (ARCHITECTURE.md §12).
package fzf

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"

	"zjump/internal/errs"
)

// ErrNotFound is returned when the fzf binary is not on PATH (R-FZF-1).
const ErrNotFound = "could not find fzf, is it installed?"

// Fzf builds an fzf invocation. Base args (applied to every run) search the path
// field only: --delimiter=\t --nth=2 --read0 (ARCHITECTURE.md §8).
type Fzf struct {
	path string
	args []string
	env  []string
}

// New resolves the fzf binary and seeds the base args. zjump requires fzf
// ≥ v0.51.0 (documented, R-FZF-6); the version is not enforced at runtime,
// matching upstream.
func New() (*Fzf, error) {
	path, err := exec.LookPath("fzf")
	if err != nil {
		return nil, errors.New(ErrNotFound)
	}
	return &Fzf{
		path: path,
		args: []string{"--delimiter=\t", "--nth=2", "--read0"},
	}, nil
}

// Args appends fzf arguments.
func (f *Fzf) Args(args ...string) *Fzf {
	f.args = append(f.args, args...)
	return f
}

// Env sets an environment variable for the child (e.g. FZF_DEFAULT_OPTS).
func (f *Fzf) Env(key, val string) *Fzf {
	f.env = append(f.env, key+"="+val)
	return f
}

// EnablePreview adds the ls-based preview pane (Unix only; a no-op elsewhere).
// Mirrors Fzf::enable_preview.
func (f *Fzf) EnablePreview() *Fzf {
	if runtime.GOOS == "windows" {
		return f
	}
	preview := `--preview=\command -p ls -Cp {2..}`
	if runtime.GOOS == "linux" {
		preview = `--preview=\command -p ls -Cp --color=always --group-directories-first {2..}`
	}
	f.Args(preview, "--preview-window=down,30%,sharp")
	// Force colorized ls output inside the (non-TTY) preview, and a POSIX shell.
	f.env = append(f.env, "CLICOLOR=1", "CLICOLOR_FORCE=1", "SHELL=sh")
	return f
}

// Spawn starts fzf with stdin/stdout piped.
func (f *Fzf) Spawn() (*Child, error) {
	cmd := exec.Command(f.path, f.args...)
	if len(f.env) > 0 {
		cmd.Env = append(cmd.Environ(), f.env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("could not open fzf stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("could not open fzf stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New(ErrNotFound)
		}
		return nil, fmt.Errorf("could not launch fzf: %w", err)
	}
	return &Child{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

// Child is a running fzf process.
type Child struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	waited bool
	result string
	err    error
}

// Write streams one candidate record (a "score\tpath" string) to fzf, appending
// the NUL terminator. If fzf has already exited (broken pipe — e.g. it accepted
// a selection under --exit-0), Write waits and returns the selection instead.
// Mirrors FzfChild::write.
func (c *Child) Write(record string) (selection *string, err error) {
	if _, werr := io.WriteString(c.stdin, record+"\x00"); werr != nil {
		if errs.IsBrokenPipe(werr) {
			sel, e := c.Wait()
			if e != nil {
				return nil, e
			}
			return &sel, nil
		}
		return nil, fmt.Errorf("could not write to fzf: %w", werr)
	}
	return nil, nil
}

// Wait closes stdin, reads fzf's stdout selection, and maps its exit code
// (ARCHITECTURE.md §8, R-FZF-5). It is safe to call more than once.
func (c *Child) Wait() (string, error) {
	if c.waited {
		return c.result, c.err
	}
	c.waited = true

	// Drop stdin to avoid deadlock, then drain stdout before reaping.
	_ = c.stdin.Close()
	out, _ := io.ReadAll(c.stdout)

	err := c.cmd.Wait()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode() // -1 if terminated by a signal
		} else {
			c.result, c.err = "", fmt.Errorf("wait failed on fzf: %w", err)
			return c.result, c.err
		}
	}

	c.result, c.err = string(out), classifyExit(code)
	if c.err != nil {
		c.result = ""
	}
	return c.result, c.err
}

// classifyExit maps an fzf process exit code to zjump's behavior (R-FZF-5,
// ARCHITECTURE.md §8). code == -1 denotes termination by a signal (Go's
// ExitCode convention), grouped with 128–254 as "terminated".
func classifyExit(code int) error {
	switch {
	case code == 0:
		return nil
	case code == 1:
		return errors.New("no match found")
	case code == 2:
		return errors.New("fzf returned an error")
	case code == 130:
		return errs.SilentExit{Code: 130}
	case code == -1 || (code >= 128 && code <= 254):
		return errors.New("fzf was terminated")
	default:
		return errors.New("fzf returned an unknown error")
	}
}
