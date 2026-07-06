package db

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeAtomic writes data to path atomically: a randomly-named temp file in the
// SAME directory, Sync, best-effort owner preservation (Unix), then rename over
// the target; the temp file is cleaned up on any failure. A crash mid-write can
// never leave the target truncated or corrupt (R-DB-2, ARCHITECTURE.md §3).
func writeAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)

	// os.CreateTemp uses O_CREATE|O_EXCL with a random name — the same
	// collision-safe atomic creation zoxide does by hand.
	tmp, err := os.CreateTemp(dir, "tmp_*")
	if err != nil {
		return fmt.Errorf("could not create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close() // may already be closed; ignore
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
