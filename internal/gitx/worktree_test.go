package gitx

import "testing"

// TestPorcelainParser exercises the fixture cases from §6 step 3: a normal
// branch worktree, a detached one, a bare block (skipped), lines that must be
// ignored (HEAD/locked/prunable), and multiple blocks (R2-WT-1).
func TestPorcelainParser(t *testing.T) {
	fixture := "" +
		"worktree /home/u/main\n" +
		"HEAD 1111111111111111111111111111111111111111\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /home/u/feature\n" +
		"HEAD 2222222222222222222222222222222222222222\n" +
		"branch refs/heads/feature\n" +
		"locked\n" +
		"\n" +
		"worktree /home/u/detached\n" +
		"HEAD 3333333333333333333333333333333333333333\n" +
		"detached\n" +
		"\n" +
		"worktree /home/u/bare-repo\n" +
		"bare\n" +
		"\n" +
		"worktree /home/u/prunable\n" +
		"HEAD 4444444444444444444444444444444444444444\n" +
		"branch refs/heads/gone\n" +
		"prunable gitdir file points to non-existent location\n"

	got := ParsePorcelain([]byte(fixture))
	want := []Worktree{
		{Path: "/home/u/main", Branch: "main"},
		{Path: "/home/u/feature", Branch: "feature"}, // locked line ignored
		{Path: "/home/u/detached", Branch: "detached"},
		// bare-repo skipped
		{Path: "/home/u/prunable", Branch: "gone"}, // prunable line ignored
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d worktrees, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("worktree %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestPorcelainParserEmpty: empty input yields no worktrees.
func TestPorcelainParserEmpty(t *testing.T) {
	if got := ParsePorcelain(nil); len(got) != 0 {
		t.Errorf("want no worktrees, got %+v", got)
	}
}

// TestPorcelainParserCRLF: carriage returns are tolerated (Windows-style output).
func TestPorcelainParserCRLF(t *testing.T) {
	fixture := "worktree /a\r\nbranch refs/heads/main\r\n"
	got := ParsePorcelain([]byte(fixture))
	if len(got) != 1 || got[0].Path != "/a" || got[0].Branch != "main" {
		t.Errorf("CRLF parse = %+v, want /a on main", got)
	}
}
