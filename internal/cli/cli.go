// Package cli implements zjump's subcommand tree, flag parsing, and dispatch on
// top of the stdlib flag package plus a small hand-rolled dispatcher (O-5,
// AGENTS.md "prefer stdlib").
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/db"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/glob"
	"github.com/primissus/zjump/internal/log"
)

// Version is zjump's version string.
const Version = "0.5.0"

// Run dispatches a subcommand. It returns nil on success, an errs.SilentExit to
// stop with a specific code and no message, or a normal error (printed by main
// as the full causal chain).
func Run(args []string) error {
	debug, logFile, rest := extractGlobalFlags(args)

	if debug {
		if err := log.Setup(logFile); err != nil {
			return fmt.Errorf("debug log %q: %w", logFile, err)
		}
		defer log.Close()
		log.Debugf("zjump %s invoked: %s", Version, strings.Join(os.Args, " "))
	}

	if len(rest) == 0 {
		printUsage(os.Stderr)
		return errs.SilentExit{Code: 1}
	}

	switch rest[0] {
	case "add":
		return runAdd(rest[1:])
	case "query":
		return runQuery(rest[1:])
	case "remove":
		return runRemove(rest[1:])
	case "init":
		return runInit(rest[1:])
	case "edit":
		return runEdit(rest[1:])
	case "alias":
		return runAlias(rest[1:])
	case "branch":
		return runBranch(rest[1:])
	case "worktree":
		return runWorktree(rest[1:])
	case "list":
		return runList(rest[1:])
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	case "-V", "--version", "version":
		fmt.Fprintf(os.Stdout, "zjump %s\n", Version)
		return nil
	default:
		return fmt.Errorf("unrecognized subcommand %q (run 'zjump --help')", rest[0])
	}
}

// defaultLogFile returns the default path for --log-file.
func defaultLogFile() string {
	return filepath.Join(os.TempDir(), "zjump-debug.log")
}

// extractGlobalFlags strips --debug/--debug=PATH and --log-file[=PATH] from args
// that appear BEFORE the subcommand token (the first non-flag positional). This
// matches the documented usage: zjump [--debug] [--log-file PATH] <COMMAND>.
func extractGlobalFlags(args []string) (debug bool, logFile string, rest []string) {
	logFile = defaultLogFile()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--debug":
			debug = true
		case strings.HasPrefix(arg, "--debug="):
			debug = true
			logFile = arg[len("--debug="):]
		case arg == "--log-file":
			i++
			if i < len(args) {
				logFile = args[i]
			}
		case strings.HasPrefix(arg, "--log-file="):
			logFile = arg[len("--log-file="):]
		default:
			rest = append(rest, arg)
			rest = append(rest, args[i+1:]...)
			return
		}
	}
	return
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
	log.Debugf("opening database: %s", dir)
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
    zjump [--debug] [--log-file PATH] <COMMAND> [OPTIONS]

Global flags:
    --debug                   Enable debug logging
    --log-file PATH           Log file path (default: `+defaultLogFile()+`)

Commands:
    add <paths>...             Add a directory or increment its rank
    query [keywords]...        Search for and print a matching directory
    remove [paths]...          Remove directories from the database
    init <bash|zsh>            Print the shell integration script
    edit                       Interactively browse/edit the database (needs fzf)
    alias [<name> <dir>]      List, create, or delete directory aliases;
                              --pick interactively selects via fzf
    branch <name> [repo...]   Print the worktree path for a checked-out branch
    worktree <name> [repo...] Print a worktree path by name or branch;
                              --all fzf-picks worktrees across all repos in DB
    list                       List directories (zoxide-like) plus opt-in aliases,
                              branches, and worktrees sections

Run 'zjump <COMMAND> --help' for command-specific options.

Environment variables:
    _ZJUMP_DATA_DIR            Directory for zjump's database file
    _ZJUMP_ECHO                Print the matched directory before navigating (=1)
    _ZJUMP_EXCLUDE_DIRS        Directory globs excluded from tracking
    _ZJUMP_FZF_OPTS            Custom flags passed to fzf (query -i only)
    _ZJUMP_MAXAGE              Aging ceiling for the total rank (default 10000)
    _ZJUMP_RESOLVE_SYMLINKS    Resolve symlinks when storing paths (=1)
    _ZJUMP_PICK_TOP            Top-N git-worktree DB entries listed when -b/-w
                              has no arg and CWD is not in a repo (default 10)
    _ZJUMP_AUTO_INDEX_DIRECTORY
                              Seed the worktrees/branches of each added repo
                              into the database on every add (=1) — see README
    _ZJUMP_DOCTOR             Disable the shell script's hook doctor check (=0)
`)
}

func printCmdHelp(w io.Writer, name, cmdUsage string) {
	fmt.Fprintf(w, "zjump %s — %s\n\n%s\n\nEnvironment variables:\n", Version, name, cmdUsage)
	fmt.Fprint(w, `    _ZJUMP_DATA_DIR            Directory for zjump's database file
    _ZJUMP_ECHO                Print the matched directory before navigating (=1)
    _ZJUMP_EXCLUDE_DIRS        Directory globs excluded from tracking
    _ZJUMP_FZF_OPTS            Custom flags passed to fzf (query -i only)
    _ZJUMP_MAXAGE              Aging ceiling for the total rank (default 10000)
    _ZJUMP_RESOLVE_SYMLINKS    Resolve symlinks when storing paths (=1)
    _ZJUMP_PICK_TOP            Top-N git-worktree DB entries listed when -b/-w
                              has no arg and CWD is not in a repo (default 10)
    _ZJUMP_AUTO_INDEX_DIRECTORY
                              Seed the worktrees/branches of each added repo
                              into the database on every add (=1) — see README
    _ZJUMP_DOCTOR             Disable the shell script's hook doctor check (=0)
`)
}
