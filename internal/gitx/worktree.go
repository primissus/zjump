package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Worktree is one entry from `git worktree list --porcelain`: its checkout path
// and a branch label. Label is the branch short name, or "detached" for a
// detached HEAD — it is never empty for a jumpable worktree (§6). Bare
// worktrees are not represented here; they are dropped during parsing.
type Worktree struct {
	Path   string
	Branch string
}

// WorktreeList runs `git -C repo worktree list --porcelain` and returns the
// parsed, jumpable worktrees. Any failure — git missing, repo no longer valid,
// nonzero exit — is returned as an error so the caller can skip the repo
// silently (§6 step 2, R2-WT-1). No shell interpolation: an explicit arg list.
func WorktreeList(repo string) ([]Worktree, error) {
	cmd := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree list failed for %s: %w", repo, err)
	}
	return ParsePorcelain(out.Bytes()), nil
}

// ParsePorcelain parses `git worktree list --porcelain` output into jumpable
// worktrees. Blocks are separated by blank lines; within a block, "worktree
// {path}" starts it, "branch refs/heads/{name}" sets the branch, a "detached"
// line labels it detached, and a "bare" line marks the block as skipped (bare
// repos are not jumpable). "locked"/"prunable"/"HEAD"/other lines are ignored.
// Exposed and pure so it can be tested against fixture text without real git
// (§6 step 3, R2-WT-1).
func ParsePorcelain(data []byte) []Worktree {
	var result []Worktree

	var (
		havePath bool
		path     string
		branch   string
		bare     bool
	)
	flush := func() {
		if havePath && !bare {
			if branch == "" {
				branch = "detached" // no branch and not bare => detached HEAD
			}
			result = append(result, Worktree{Path: path, Branch: branch})
		}
		havePath, path, branch, bare = false, "", "", false
	}

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			// A new "worktree" line without a preceding blank still starts a new
			// block; flush any block in progress first (defensive).
			flush()
			havePath = true
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "detached":
			branch = "detached"
		case line == "bare":
			bare = true
		default:
			// HEAD, locked, prunable, and anything else: ignored.
		}
	}
	flush() // final block (input may not end with a blank line)
	return result
}
