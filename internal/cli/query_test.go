package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/primissus/zjump/internal/db"
)

func nowEpoch() db.Epoch { return db.Epoch(time.Now().Unix()) }

// preload builds a database under the current data dir via fn and saves it.
func preload(t *testing.T, fn func(*db.Database)) {
	t.Helper()
	database := openTestDB(t)
	fn(database)
	if err := database.Save(); err != nil {
		t.Fatal(err)
	}
}

// runQueryCapture runs `query args...` capturing stdout.
func runQueryCapture(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return captureStdout(t, func() error { return runQuery(args) })
}

// TestQueryTypeFilter: each --type value selects the right candidate kinds,
// `any` spans DB kinds plus store aliases, and an unknown value is rejected
// (§5.1, R2-TYPE-1/2/3).
func TestQueryTypeFilter(t *testing.T) {
	setupDataDir(t)
	aliasTarget := t.TempDir()
	preload(t, func(d *db.Database) {
		d.AddUpdate("/plain/dir", 1.0, nowEpoch(), db.KindDir)
		d.AddUpdate("/some/repo", 1.0, nowEpoch(), db.KindRepo)
	})
	// Seed the alias store (separate store — A-1).
	if err := runAlias([]string{"myalias", aliasTarget}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		typ         string
		wantContain []string
		wantAbsent  []string
	}{
		{"dir", []string{"/plain/dir"}, []string{"/some/repo", aliasTarget}},
		{"repo", []string{"/some/repo"}, []string{"/plain/dir", aliasTarget}},
		{"alias", []string{aliasTarget}, []string{"/plain/dir", "/some/repo"}},
		{"any", []string{"/plain/dir", "/some/repo", aliasTarget}, nil},
		{"", []string{"/plain/dir", "/some/repo"}, []string{aliasTarget}}, // default: dir+repo
	}
	for _, c := range cases {
		args := []string{"-l", "--all"}
		if c.typ != "" {
			args = append(args, "--type", c.typ)
		}
		out, err := runQueryCapture(t, args...)
		if err != nil {
			t.Fatalf("type %q: %v", c.typ, err)
		}
		for _, want := range c.wantContain {
			if !strings.Contains(out, want) {
				t.Errorf("type %q output %q missing %q", c.typ, out, want)
			}
		}
		for _, absent := range c.wantAbsent {
			if strings.Contains(out, absent) {
				t.Errorf("type %q output %q should not contain %q", c.typ, out, absent)
			}
		}
	}

	// Invalid --type value is a hard error.
	if err := runQuery([]string{"--type", "bogus"}); err == nil || err.Error() != "invalid type: bogus" {
		t.Errorf("invalid type error = %v, want 'invalid type: bogus'", err)
	}
}
