package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"zjump/internal/config"
	"zjump/internal/db"
	"zjump/internal/errs"
	"zjump/internal/paths"
)

// runAlias implements `zjump alias add|rm|list` — user-named shortcuts that
// resolve to paths, a capability zoxide lacks (G-6/G-7, D-6). Aliases live in the
// same database as a distinct kind and are managed only through this command
// (creation/replacement/removal) and the on-use fast-path bump (§5.4).
func runAlias(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("alias: a subcommand is required (add, rm, or list)")
	}
	switch args[0] {
	case "add":
		return runAliasAdd(args[1:])
	case "rm":
		return runAliasRm(args[1:])
	case "list":
		return runAliasList(args[1:])
	default:
		return fmt.Errorf("unrecognized alias subcommand: %s", args[0])
	}
}

// validateAliasName enforces the name rules in §5.4 (error `invalid alias name:
// {name}`): non-empty; no `/`, no whitespace; must not start with `-`; must not
// be exactly `.`, `..`, `-`, or `~` (the shell function's cd-idiom branches
// consume those before zjump ever sees them).
func validateAliasName(name string) error {
	bad := func() error { return fmt.Errorf("invalid alias name: %s", name) }
	switch name {
	case "", ".", "..", "-", "~":
		return bad()
	}
	if strings.HasPrefix(name, "-") || strings.ContainsRune(name, '/') {
		return bad()
	}
	for _, r := range name {
		if unicode.IsSpace(r) { // covers spaces, tabs, and \n/\r
			return bad()
		}
	}
	return nil
}

// runAliasAdd implements `zjump alias add <NAME> [PATH]`. PATH defaults to the
// current directory and is resolved exactly as `zjump add` does (§5.4).
func runAliasAdd(args []string) error {
	fs := newFlagSet("alias add")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 || len(rest) > 2 {
		return fmt.Errorf("alias add: usage: alias add <NAME> [PATH]")
	}
	name := rest[0]
	if err := validateAliasName(name); err != nil {
		return err
	}

	target := "."
	if len(rest) == 2 {
		target = rest[1]
	}

	var resolved string
	if config.ResolveSymlinks() {
		resolved, err = paths.Canonicalize(target)
	} else {
		resolved, err = paths.ResolvePath(target)
	}
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(resolved); statErr != nil || !info.IsDir() {
		return fmt.Errorf("not a directory: %s", resolved)
	}

	now, err := paths.CurrentTime()
	if err != nil {
		return err
	}
	database, err := openDB()
	if err != nil {
		return err
	}
	database.PutAlias(name, resolved, now)
	return database.Save()
}

// runAliasRm implements `zjump alias rm <NAME>` (§5.4).
func runAliasRm(args []string) error {
	fs := newFlagSet("alias rm")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("alias rm: usage: alias rm <NAME>")
	}
	name := rest[0]

	database, err := openDB()
	if err != nil {
		return err
	}
	if !database.RemoveAlias(name) {
		return fmt.Errorf("alias not found: %s", name)
	}
	return database.Save()
}

// runAliasList implements `zjump alias list [--score]`: rows `name\tpath` (with a
// leading score column under --score), sorted best decayed score first, with NO
// dedup by target — every alias shows (§5.4).
func runAliasList(args []string) error {
	fs := newFlagSet("alias list")
	var score bool
	fs.BoolVar(&score, "score", false, "")
	fs.BoolVar(&score, "s", false, "")
	if _, err := parseArgs(fs, args); err != nil {
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

	// Collect aliases, best decayed score first; ties break by name for a
	// deterministic order.
	var aliases []db.Dir
	for _, d := range database.Dirs() {
		if d.IsAlias() {
			aliases = append(aliases, d)
		}
	}
	sort.SliceStable(aliases, func(i, j int) bool {
		si, sj := aliases[i].Score(now), aliases[j].Score(now)
		if si != sj {
			return si > sj
		}
		return aliases[i].Name < aliases[j].Name
	})

	for i := range aliases {
		a := &aliases[i]
		row := a.Name + "\t" + a.Path
		if score {
			row = fmt.Sprintf("%6.1f\t%s", clampScore(a.Score(now)), row)
		}
		if _, werr := fmt.Fprintln(os.Stdout, row); werr != nil {
			return errs.PipeExit(werr, "stdout")
		}
	}
	return nil
}

// clampScore clamps a decayed score into the display range [0, 9999], matching
// the DirDisplay formatting used elsewhere.
func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 9999 {
		return 9999
	}
	return v
}
