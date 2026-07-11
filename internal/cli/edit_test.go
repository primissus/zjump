package cli

import (
	"strings"
	"testing"

	"zjump/internal/db"
)

// TestEditReloadOmitsAliases: the `edit reload` dump lists dir/repo entries but
// never aliases, so the edit UI can't display or mutate them (§3, R2-IDX-2).
func TestEditReloadOmitsAliases(t *testing.T) {
	setupDataDir(t)

	// Preload a database with two path entries and one alias.
	database := openTestDB(t)
	database.AddUpdate("/home/alice/work", 3.0, 1000, db.KindDir)
	database.AddUpdate("/home/alice/repo", 5.0, 1000, db.KindRepo)
	database.PutAlias("shortcut", "/home/alice/aliased-target", 1000)
	if err := database.Save(); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error { return runEdit([]string{"reload"}) })
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "/home/alice/work") || !strings.Contains(out, "/home/alice/repo") {
		t.Errorf("reload dump missing dir/repo entries: %q", out)
	}
	if strings.Contains(out, "shortcut") || strings.Contains(out, "aliased-target") {
		t.Errorf("reload dump leaked an alias: %q", out)
	}
}
