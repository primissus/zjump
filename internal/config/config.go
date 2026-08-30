// Package config reads and validates zjump's _ZJUMP_* environment variables
// and resolves the data directory. Each mirrors zoxide's _ZO_* semantics
// one-for-one (REQUIREMENTS.md §2.9, D-2).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/primissus/zjump/internal/glob"
)

// DataDir resolves the directory holding the database file. Uses _ZJUMP_DATA_DIR
// if set, else the platform local-data dir joined with "zjump". The result must
// be absolute — validated unconditionally (R-ENV-1). Mirrors config::data_dir.
func DataDir() (string, error) {
	var dir string
	if v, ok := os.LookupEnv("_ZJUMP_DATA_DIR"); ok {
		dir = v
	} else {
		base, err := dataLocalDir()
		if err != nil {
			return "", fmt.Errorf("could not find data directory, please set _ZJUMP_DATA_DIR manually: %w", err)
		}
		dir = filepath.Join(base, "zjump")
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("_ZJUMP_DATA_DIR must be an absolute path")
	}
	return dir, nil
}

// dataLocalDir returns the platform local-data directory, mirroring
// dirs::data_local_dir on Unix targets (Linux/BSD via XDG, macOS via
// ~/Library/Application Support).
func dataLocalDir() (string, error) {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return xdg, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// Echo reports whether to print the matched directory before navigating. True
// only when the value is exactly "1" (R-ENV-2). Consumed by init templates.
func Echo() bool { return os.Getenv("_ZJUMP_ECHO") == "1" }

// ResolveSymlinks reports whether to resolve symlinks in add/query. True only
// when the value is exactly "1" (R-ENV-6).
func ResolveSymlinks() bool { return os.Getenv("_ZJUMP_RESOLVE_SYMLINKS") == "1" }

// AutoIndexDirectory reports whether `add` should also seed the worktrees (and
// therefore branches) of the repository containing each added path into the
// frecency database, once each, so they become jumpable without a prior visit.
// True only when the value is exactly "1". zjump extension (no zoxide analog).
func AutoIndexDirectory() bool { return os.Getenv("_ZJUMP_AUTO_INDEX_DIRECTORY") == "1" }

// ExcludeDirs returns the exclude globs. If _ZJUMP_EXCLUDE_DIRS is set, it is an
// OS path-list of glob patterns; if unset, it defaults to a single pattern
// matching the home directory literally (glob-escaped, non-recursive). Mirrors
// config::exclude_dirs (R-ENV-3).
func ExcludeDirs() ([]*glob.Glob, error) {
	if v, ok := os.LookupEnv("_ZJUMP_EXCLUDE_DIRS"); ok {
		var globs []*glob.Glob
		for _, pattern := range filepath.SplitList(v) {
			g, err := glob.New(pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid glob in _ZJUMP_EXCLUDE_DIRS: %s: %w", pattern, err)
			}
			globs = append(globs, g)
		}
		return globs, nil
	}
	// Default: match the home directory literally (best-effort, like zoxide's
	// silently-dropped Option on failure).
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	g, err := glob.New(glob.Escape(home))
	if err != nil {
		return nil, nil
	}
	return []*glob.Glob{g}, nil
}

// FzfOpts returns the raw _ZJUMP_FZF_OPTS value and whether it was set. Read by
// `query -i` only, never by `edit` (R-ENV-4, R-FZF-4).
func FzfOpts() (string, bool) { return os.LookupEnv("_ZJUMP_FZF_OPTS") }

// Maxage returns the aging ceiling: _ZJUMP_MAXAGE parsed as u32 then treated as
// float, default 10000 (R-ENV-5). Mirrors config::maxage.
func Maxage() (float64, error) {
	v, ok := os.LookupEnv("_ZJUMP_MAXAGE")
	if !ok {
		return 10000.0, nil
	}
	// Rust's u32::from_str accepts an optional leading '+'; strconv.ParseUint
	// does not, so strip one to keep parity with zoxide (R-ENV-5).
	parseTarget := strings.TrimPrefix(v, "+")
	n, err := strconv.ParseUint(parseTarget, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("unable to parse _ZJUMP_MAXAGE as integer: %s", v)
	}
	return float64(n), nil
}

// PickTop returns the top-N count for the git-worktree DB-fallback used by
// `branch`/`worktree` when called without arguments and CWD is not inside a git
// repository. Reads _ZJUMP_PICK_TOP; defaults to 10. Must be a positive integer.
func PickTop() (int, error) {
	v, ok := os.LookupEnv("_ZJUMP_PICK_TOP")
	if !ok {
		return 10, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("unable to parse _ZJUMP_PICK_TOP as integer: %s", v)
	}
	if n <= 0 {
		return 0, fmt.Errorf("_ZJUMP_PICK_TOP must be a positive integer: %s", v)
	}
	return n, nil
}
