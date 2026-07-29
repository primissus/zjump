package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/fzf"
	"github.com/primissus/zjump/internal/paths"
)

const editHelp = `Usage: zjump edit [<subcommand> <args>...]

Interactively browse and edit the frecency database via fzf.

Subcommands (used by fzf key bindings):
    increment <path>    Increment the rank of a path
    decrement <path>    Decrement the rank of a path
    delete <path>       Delete a path from the database
    reload              Reload the display list

Without a subcommand, launches the fzf interactive browser.
`

// runEdit implements `zjump edit`. With no subcommand it launches the fzf-driven
// browser; the hidden increment/decrement/delete/reload subcommands back its key
// bindings (R-EDIT-*). Reads the clock, so it has a clock-error case (R-ERR-4).
//
// Deviation D-3: after every mutating reload we re-sort by score before dumping,
// so the list never goes stale mid-session (fixing zoxide's swap_remove sort
// staleness). The mutation itself marks the DB dirty; SortByScore does not
// (D-4), so the save persists exactly the mutation.
func runEdit(args []string) error {
	fs := newFlagSet("edit")
	rest, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "edit", editHelp)
			return nil
		}
		return err
	}

	now, err := paths.CurrentTime()
	if err != nil {
		return err
	}
	database, err := openDB()
	if err != nil {
		return err
	}

	if len(rest) == 0 {
		return editInteractive(database, now)
	}

	sub := rest[0]
	switch sub {
	case "increment":
		if len(rest) < 2 {
			return fmt.Errorf("edit increment: a path argument is required")
		}
		database.Add(rest[1], 1.0, now)
	case "decrement":
		if len(rest) < 2 {
			return fmt.Errorf("edit decrement: a path argument is required")
		}
		database.Add(rest[1], -1.0, now)
	case "delete":
		if len(rest) < 2 {
			return fmt.Errorf("edit delete: a path argument is required")
		}
		database.Remove(rest[1])
	case "reload":
		// Pure no-op mutation; used only to re-dump the list.
	default:
		return fmt.Errorf("unrecognized edit subcommand: %s", sub)
	}

	if err := database.Save(); err != nil {
		return err
	}
	// D-3: re-sort so the re-dumped list is always correctly ordered best-first.
	database.SortByScore(now)
	return dumpEntries(database, now)
}

// editInteractive launches the fzf browser. The initial list is populated by the
// start:reload binding (a fresh `zjump edit reload`), so the parent process just
// spawns fzf and waits; the return value is discarded (R-EDIT-1).
func editInteractive(database *db.Database, now db.Epoch) error {
	database.SortByScore(now)
	if err := database.Save(); err != nil { // no-op under D-4 unless already dirty
		return err
	}
	child, err := editFzf()
	if err != nil {
		return err
	}
	_, err = child.Wait()
	return err
}

// dumpEntries writes every entry as a "score\tpath\0" record, best-first, for
// fzf's next candidate list (R-EDIT-2).
func dumpEntries(database *db.Database, now db.Epoch) error {
	dirs := database.Dirs()
	for i := len(dirs) - 1; i >= 0; i-- {
		rec := dirs[i].DisplayScore(now, "\t") + "\x00"
		if _, werr := fmt.Fprint(os.Stdout, rec); werr != nil {
			return errs.PipeExit(werr, "fzf")
		}
	}
	return nil
}

// editFzf builds the edit browser's fzf invocation: fixed key bindings, a header
// legend, and an always-on preview pane (independent of _ZJUMP_FZF_OPTS, which
// edit never reads — R-EDIT-3, R-FZF-4). Mirrors cmd/edit.rs.
func editFzf() (*fzf.Child, error) {
	f, err := fzf.New()
	if err != nil {
		return nil, err
	}
	f.Args(
		"--exact",
		"--no-sort",
		"--bind=btab:up,"+
			"ctrl-r:reload(zjump edit reload),"+
			"ctrl-d:reload(zjump edit delete {2..}),"+
			"ctrl-w:reload(zjump edit increment {2..}),"+
			"ctrl-s:reload(zjump edit decrement {2..}),"+
			"ctrl-z:ignore,"+
			"double-click:ignore,"+
			"enter:abort,"+
			"start:reload(zjump edit reload),"+
			"tab:down",
		"--cycle",
		"--keep-right",
		"--border=sharp",
		"--border-label=  zjump-edit  ",
		"--header=ctrl-r:reload   \tctrl-d:delete\nctrl-w:increment\tctrl-s:decrement\n\n SCORE\tPATH",
		"--info=inline",
		"--layout=reverse",
		"--padding=1,0,0,0",
		"--color=label:bold",
		"--tabstop=1",
	)
	f.EnablePreview()
	return f.Spawn()
}
