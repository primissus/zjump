package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn and returns its stdout output.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	err = fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, cerr := io.Copy(&buf, r); cerr != nil {
		t.Fatal(cerr)
	}
	return buf.String(), err
}

func TestInitNoDebug(t *testing.T) {
	out, err := captureStdout(t, func() error { return runInit([]string{"zsh"}) })
	if err != nil {
		t.Fatal(err)
	}
	// Verify __zjump wrapper is present without debug flags.
	if strings.Contains(out, `\command zjump --debug`) {
		t.Error("expected no debug wrapper without --debug flag, got:\n", out)
	}
	if !strings.Contains(out, `__zjump() {
    \command zjump "$@"`) {
		t.Error("expected __zjump wrapper without debug, got:\n", out)
	}
}

func TestInitDebugDefault(t *testing.T) {
	out, err := captureStdout(t, func() error { return runInit([]string{"--debug", "zsh"}) })
	if err != nil {
		t.Fatal(err)
	}
	// Default log path should appear in the wrapper.
	def := defaultLogFile()
	if !strings.Contains(out, def) {
		t.Errorf("expected default log path %q in output, got:\n%s", def, out)
	}
	// Verify __zjump wrapper has the debug form.
	if !strings.Contains(out, `\command zjump --debug --log-file "`) {
		t.Error("expected __zjump wrapper with debug flags")
	}
}

func TestInitDebugCustomPath(t *testing.T) {
	custom := "/tmp/zjump-test-custom.log"
	out, err := captureStdout(t, func() error { return runInit([]string{"--debug=" + custom, "zsh"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, custom) {
		t.Errorf("expected custom log path %q in output, got:\n%s", custom, out)
	}
}

func TestInitDebugRelativePath(t *testing.T) {
	out, err := captureStdout(t, func() error { return runInit([]string{"--debug=mydebug.log", "zsh"}) })
	if err != nil {
		t.Fatal(err)
	}
	// After resolution it should be an absolute path (containing /).
	if !strings.Contains(out, "/mydebug.log") && !strings.Contains(out, "\\mydebug.log") {
		t.Errorf("expected absolute path for relative input, got:\n%s", out)
	}
}

func TestInitDebugBash(t *testing.T) {
	out, err := captureStdout(t, func() error { return runInit([]string{"--debug", "bash"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `\command zjump --debug --log-file "`) {
		t.Error("expected debug wrapper in bash output")
	}
}

func TestInitDebugBareAfterSubcommand(t *testing.T) {
	// --debug after the subcommand should bake, not be consumed as runtime.
	out, err := captureStdout(t, func() error { return runInit([]string{"--debug", "zsh"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `\command zjump --debug --log-file "`) {
		t.Errorf("expected baked debug wrapper, got:\n%s", out)
	}
}

func TestInitDebugUnsafePath(t *testing.T) {
	// H1: the log path is baked verbatim into the eval'd template, so
	// shell-unsafe characters must be rejected.
	for _, p := range []string{
		`/tmp/a"b.log`,
		"/tmp/a'b.log",
		"/tmp/a`b.log",
		`/tmp/a\b.log`,
		"/tmp/a$b.log",
		"/tmp/a\nb.log",
	} {
		_, err := captureStdout(t, func() error { return runInit([]string{"--debug=" + p, "zsh"}) })
		if err == nil || !strings.Contains(err.Error(), "shell-unsafe") {
			t.Errorf("runInit(--debug=%q) err = %v, want shell-unsafe error", p, err)
		}
	}
}
