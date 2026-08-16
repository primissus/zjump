// Package cli implements zjump's subcommand tree, flag parsing, and dispatch on
// top of the stdlib flag package plus a small hand-rolled dispatcher (O-5,
// AGENTS.md "prefer stdlib").
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"zjump/internal/config"
	"zjump/internal/db"
	"zjump/internal/errs"
	"zjump/internal/glob"
)

// Version is zjump's version string. May be overridden at link time via
// -ldflags "-X zjump/internal/cli.Version=<v>" (used by GoReleaser releases).
var Version = "0.1.0-dev"

// Run dispatches a subcommand. It returns nil on success, an errs.SilentExit to
// stop with a specific code and no message, or a normal error (printed by main
// as the full causal chain).
func Run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return errs.SilentExit{Code: 1}
	}

	switch args[0] {
	case "add":
		return runAdd(args[1:])
	case "query":
		return runQuery(args[1:])
	case "remove":
		return runRemove(args[1:])
	case "init":
		return runInit(args[1:])
	case "edit":
		return runEdit(args[1:])
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	case "-V", "--version", "version":
		fmt.Fprintf(os.Stdout, "zjump %s\n", Version)
		return nil
	default:
		return fmt.Errorf("unrecognized subcommand %q (run 'zjump --help')", args[0])
	}
}

// newFlagSet returns a ContinueOnError flag set that discards flag's own output
// so parse errors propagate as returned errors (printed once, by main).
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseArgs parses flags that may be interspersed with positionals, returning
// the positionals in order. Everything after the first "--" is treated as a
// literal positional (never a flag) — matching how the generated shell
// functions always pass `-- "$@"` (R-INIT-5). The stdlib flag package alone
// stops at the first positional; this permutes so `query foo -i` works too.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var literal []string
	for i, a := range args {
		if a == "--" {
			literal = append(literal, args[i+1:]...)
			args = args[:i]
			break
		}
	}

	var positionals []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		positionals = append(positionals, args[0])
		args = args[1:]
	}
	return append(positionals, literal...), nil
}

// openDB resolves the data directory and opens the database.
func openDB() (*db.Database, error) {
	dir, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	return db.OpenDir(dir)
}

// matchesAny reports whether path matches any of the exclude globs.
func matchesAny(globs []*glob.Glob, path string) bool {
	for _, g := range globs {
		if g.Match(path) {
			return true
		}
	}
	return false
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `zjump `+Version+` — a frecency-based cd replacement (a Go reimplementation of zoxide)

Usage:
    zjump <COMMAND> [OPTIONS]

Commands:
    add <paths>...             Add a directory or increment its rank
    query [keywords]...        Search for and print a matching directory
    remove [paths]...          Remove directories from the database
    init <bash|zsh>            Print the shell integration script
    edit                       Interactively browse/edit the database (needs fzf)

Run 'zjump <COMMAND> --help' for command-specific options.

Environment variables:
    _ZJUMP_DATA_DIR            Directory for zjump's database file
    _ZJUMP_ECHO                Print the matched directory before navigating (=1)
    _ZJUMP_EXCLUDE_DIRS        Directory globs excluded from tracking
    _ZJUMP_FZF_OPTS            Custom flags passed to fzf (query -i only)
    _ZJUMP_MAXAGE              Aging ceiling for the total rank (default 10000)
    _ZJUMP_RESOLVE_SYMLINKS    Resolve symlinks when storing paths (=1)
`)
}
