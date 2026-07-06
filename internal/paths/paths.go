// Package paths provides zjump's two distinct, non-interchangeable
// path-normalization primitives (ARCHITECTURE.md §6) plus the current-time
// reader. Unix-only (N-4).
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"zjump/internal/db"
)

// ResolvePath returns the absolute form of p WITHOUT touching the filesystem and
// WITHOUT resolving symlinks — purely lexical: relative paths are joined onto the
// current working directory and `.`/`..` are normalized as plain components.
// Mirrors util::resolve_path (Unix branch). Used by `add` (lexical mode) and as
// `remove`'s fallback lookup (R-ADD-6, R-RM-2).
func ResolvePath(p string) (string, error) {
	var stack []string // stack[0] == "/" once initialized
	if strings.HasPrefix(p, "/") {
		stack = []string{"/"}
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("could not get current directory: %w", err)
		}
		stack = append(stack, "/")
		stack = append(stack, nonRootComponents(cwd)...)
	}

	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
			// Skip empty segments (leading/duplicate '/') and CurDir.
		case "..":
			if len(stack) > 1 { // never pop past root
				stack = stack[:len(stack)-1]
			}
		default:
			stack = append(stack, seg)
		}
	}

	if len(stack) == 1 {
		return "/", nil
	}
	return "/" + strings.Join(stack[1:], "/"), nil
}

// Canonicalize returns the fully-resolved absolute path with all symlinks
// followed; it REQUIRES the path to exist (errors otherwise), matching
// dunce::canonicalize on Unix. Used by `add` when _ZJUMP_RESOLVE_SYMLINKS=1
// (R-ADD-6).
func Canonicalize(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("could not resolve path: %s: %w", p, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("could not resolve path: %s: %w", p, err)
	}
	return resolved, nil
}

// CurrentTime returns the current Unix time in whole seconds, erroring if the
// system clock predates the Unix epoch (R-ADD-9, R-ERR-4). Mirrors
// util::current_time.
func CurrentTime() (db.Epoch, error) {
	sec := time.Now().Unix()
	if sec < 0 {
		return 0, fmt.Errorf("system clock set to invalid time")
	}
	return db.Epoch(sec), nil
}

// nonRootComponents splits an absolute, cleaned path into its components,
// excluding the root and any empty segments.
func nonRootComponents(abs string) []string {
	trimmed := strings.Trim(abs, "/")
	if trimmed == "" {
		return nil
	}
	var comps []string
	for _, seg := range strings.Split(trimmed, "/") {
		if seg != "" {
			comps = append(comps, seg)
		}
	}
	return comps
}
