package cli

import (
	"fmt"

	"zjump/internal/db"
)

// queryWorktree implements `zjump query --type worktree` — the live worktree
// enumeration over indexed repositories (§6, D-6). No git subprocess runs unless
// this type is requested (§6 step 8).
//
// The live enumeration (streaming KindRepo entries best-first, running
// `git worktree list --porcelain`, parsing/dedup/50-repo cap and the three-field
// interactive records) is delivered in Phase 5 (R2-WT-1/2). Until then the flag
// is accepted and validated (R2-TYPE-1) but produces no candidates.
func queryWorktree(database *db.Database, p queryParams, now db.Epoch) error {
	switch {
	case p.list:
		return nil // no candidates yet; an empty list is not an error
	default:
		return fmt.Errorf("no match found")
	}
}
