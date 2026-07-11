package gitx

import (
	"bytes"
	"os/exec"
	"strings"
)

// runGit runs `git args...` in the current working directory and returns its
// stdout. All branch/worktree helpers use explicit arg lists — no shell.
func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

// InsideWorkTree reports whether the current directory is inside a git work
// tree: `git rev-parse --is-inside-work-tree` must succeed and print "true".
// Any failure or other output means "not inside" (§5.5 step 1, R2-BR-1).
func InsideWorkTree() bool {
	out, err := runGit("rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

// LocalBranches lists the current repo's local branches, most-recently-committed
// first (`git for-each-ref refs/heads --sort=-committerdate
// --format=%(refname:short)`). The result may be empty on an unborn HEAD (§5.5
// step 2, R2-BR-1).
func LocalBranches() ([]string, error) {
	out, err := runGit("for-each-ref", "refs/heads", "--sort=-committerdate", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			branches = append(branches, s)
		}
	}
	return branches, nil
}

// CurrentBranch returns the checked-out branch name, or "" on a detached HEAD
// (`git branch --show-current`) (§5.5 step 3, R2-BR-1).
func CurrentBranch() string {
	out, err := runGit("branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
