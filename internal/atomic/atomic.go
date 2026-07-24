// Package atomic provides a crash-safe atomic file writer used by the database
// and alias store. Mirrors zoxide's atomic write semantics: write a temp file in
// the same directory, sync, best-effort preserve ownership, then rename over the
// target (R-DB-2, ARCHITECTURE.md §3).
package atomic

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write writes data to path atomically: a randomly-named temp file in the
// same directory, Sync, best-effort owner preservation (Unix), then rename over
// the target; the temp file is cleaned up on any failure. A crash mid-write can
// never leave the target truncated or corrupt.
func Write(path string, data []byte) (err error) {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, "tmp_*")
	if err != nil {
		return fmt.Errorf("could not create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("could not write to file: %s: %w", tmpName, err)
	}

	// Best-effort: don't silently change ownership on rewrite (Unix only).
	preserveOwner(tmp, path)

	// Sync before close so any deferred write error surfaces here rather than
	// being dropped by an ignored close() inside a finalizer.
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("could not sync writes to file: %s: %w", tmpName, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("could not close file: %s: %w", tmpName, err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("could not rename file: %s -> %s: %w", tmpName, path, err)
	}
	return nil
}
