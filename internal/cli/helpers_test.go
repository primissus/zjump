package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"zjump/internal/db"
)

// setupDataDir points zjump at a fresh temp database and neutralizes the
// environment (empty exclude list, no symlink resolution) so tests are
// deterministic regardless of the developer's shell.
func setupDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("_ZJUMP_DATA_DIR", dir)
	t.Setenv("_ZJUMP_EXCLUDE_DIRS", "") // no excludes (overrides the home-dir default)
	t.Setenv("_ZJUMP_RESOLVE_SYMLINKS", "")
	return dir
}

// mkRepo creates dir (and parents) with a ".git" subdirectory so gitx.IsRepoRoot
// reports it as a repository — no real git needed.
func mkRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// openTestDB opens the database under the current _ZJUMP_DATA_DIR.
func openTestDB(t *testing.T) *db.Database {
	t.Helper()
	database, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	return database
}

// captureStdout redirects os.Stdout for the duration of fn and returns what was
// written.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	return captureFD(t, &os.Stdout, fn)
}

// captureStderr redirects os.Stderr for the duration of fn and returns what was
// written.
func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	return captureFD(t, &os.Stderr, fn)
}

// captureFD swaps *fd for a pipe around fn and returns everything written to it.
func captureFD(t *testing.T, fd **os.File, fn func() error) (string, error) {
	t.Helper()
	orig := *fd
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	*fd = w
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	fnErr := fn()

	_ = w.Close()
	*fd = orig
	out := <-done
	_ = r.Close()
	return out, fnErr
}
