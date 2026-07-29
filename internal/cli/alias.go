package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/primissus/zjump/internal/alias"
	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/paths"
)

const aliasHelp = `Usage: zjump alias [options] [<name> <dir>]

List, create, or delete directory aliases.

With no arguments, list all aliases (name<tab>path per line).
With <name> <dir>, create or update an alias.
With --delete <name>, remove an alias.

Flags:
    -d, --delete NAME   Delete an alias by name
`

// runAlias implements `zjump alias`. With no args it lists; with a name and
// directory it creates/overwrites; with -d/--delete it removes.
func runAlias(args []string) error {
	fs := newFlagSet("alias")
	var delete bool
	fs.BoolVar(&delete, "d", false, "")
	fs.BoolVar(&delete, "delete", false, "")

	rest, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "alias", aliasHelp)
			return nil
		}
		return err
	}

	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}
	store, err := alias.Open(dataDir)
	if err != nil {
		return err
	}

	if delete {
		if len(rest) != 1 {
			return fmt.Errorf("alias --delete: exactly one name is required")
		}
		if !store.Delete(rest[0]) {
			return fmt.Errorf("alias not found: %s", rest[0])
		}
		return store.Save()
	}

	switch len(rest) {
	case 0:
		for _, e := range store.Entries() {
			if _, werr := fmt.Fprintf(os.Stdout, "%s\t%s\n", e.Name, e.Path); werr != nil {
				return errs.PipeExit(werr, "stdout")
			}
		}
		return nil
	case 2:
		name := rest[0]
		target := rest[1]

		if err := alias.ValidateName(name); err != nil {
			return err
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
		if !utf8.ValidString(resolved) {
			return fmt.Errorf("invalid unicode in path: %s", resolved)
		}
		if strings.ContainsAny(resolved, "\n\r") {
			return fmt.Errorf("alias path must not contain newline or carriage-return")
		}
		if info, statErr := os.Stat(resolved); statErr != nil || !info.IsDir() {
			return fmt.Errorf("not a directory: %s", resolved)
		}

		store.Set(name, resolved)
		return store.Save()
	default:
		return fmt.Errorf("alias: expected 0 args (list) or 2 args (name, directory), got %d", len(rest))
	}
}
