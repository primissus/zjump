// Package gitx holds zjump's git integration: repo-root detection, worktree
// enumeration, and branch listing. It is a capability zoxide lacks (D-6). All
// git invocations use os/exec with explicit argument lists — no shell
// interpolation — and the package depends only on the standard library
// (PLAN-GIT.md §6, ARCHITECTURE.md §12).
package gitx

import (
	"os"
	"path/filepath"
)

// IsRepoRoot reports whether path is the root of a git repository: it is iff
// "<path>/.git" exists as a directory OR a regular file. The file form is what a
// linked worktree's root carries, so a visited worktree root counts as a repo
// root too and can enumerate its siblings (PLAN-GIT.md §2, R2-IDX-1).
//
// This is a single os.Lstat — no `git` subprocess. Lstat (not Stat) so a `.git`
// that is itself a symlink is not silently followed and mistaken for a repo.
func IsRepoRoot(path string) bool {
	info, err := os.Lstat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	mode := info.Mode()
	return mode.IsDir() || mode.IsRegular()
}
