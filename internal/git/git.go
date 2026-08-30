// Package git shells out to the `git` binary for repository and worktree
// operations that back the `zjump branch` and `zjump worktree` subcommands.
// Parsing functions are pure and tested without git.
package git

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsRepoRoot reports whether path is the root of a git repository: it is iff
// "<path>/.git" exists as a directory OR a regular file. The file form is what a
// linked worktree's root carries, so a visited worktree root counts as a repo
// root too and can enumerate its siblings (PLAN-GIT.md §2, R2-IDX-1).
//
// This is a single os.Lstat — no `git` subprocess. Lstat (not Stat) so a `.git`
// that is itself a symlink is not silently followed and mistaken for a repo.
// zjump extension (D-6): zoxide has no repo-root concept.
func IsRepoRoot(path string) bool {
	info, err := os.Lstat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	mode := info.Mode()
	return mode.IsDir() || mode.IsRegular()
}

// Worktree describes one entry from `git worktree list --porcelain`.
type Worktree struct {
	Path     string
	Head     string
	Branch   string // short name, e.g. "main"; empty when detached
	Detached bool
}

// RepoRoot runs `git -C dir rev-parse --show-toplevel` and returns the absolute
// repository path with trailing whitespace trimmed.
func RepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			return "", fmt.Errorf("could not find git, is it installed?")
		}
		return "", fmt.Errorf("not a git repository: %s", dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// Worktrees runs `git -C dir worktree list --porcelain` and returns the parsed
// entries (including the main worktree).
func Worktrees(dir string) ([]Worktree, error) {
	cmd := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			return nil, fmt.Errorf("could not find git, is it installed?")
		}
		return nil, fmt.Errorf("not a git repository: %s", dir)
	}
	return parseWorktrees(string(out))
}

// CurrentBranch runs `git -C dir rev-parse --abbrev-ref HEAD` and returns the
// branch shortname. Returns "HEAD" for detached HEAD, "" on error.
func CurrentBranch(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseWorktrees reads `git worktree list --porcelain` output and returns the
// parsed entries. It is used by Worktrees and tested independently.
func parseWorktrees(output string) ([]Worktree, error) {
	var wts []Worktree
	var cur *Worktree

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if cur != nil {
				wts = append(wts, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			cur = &Worktree{}
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			key = line
			value = ""
		}
		switch key {
		case "worktree":
			cur.Path = value
		case "HEAD":
			cur.Head = value
		case "branch":
			cur.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			cur.Detached = true
		case "bare":
			// ignore
		default:
			// ignore unknown keys for forward compatibility
		}
	}
	if cur != nil {
		wts = append(wts, *cur)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return wts, nil
}


