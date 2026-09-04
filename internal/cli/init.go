package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/primissus/zjump/internal/config"
	"github.com/primissus/zjump/internal/errs"
	"github.com/primissus/zjump/internal/shell"
)

var initHelp = `Usage: zjump init [OPTIONS] <bash|zsh>

Generate and print the shell integration script. Pipe or eval
the output in your shell's rc file.

Flags:
    --cmd NAME          Shell command name (default: zz)
    --no-cmd            Do not define any shell command
    --no-aliases        Alias for --no-cmd
    --hook HOOK         Hook mode: none, prompt, pwd (default: pwd)
    --debug[=PATH]      Bake debug logging into the generated script;
                        if PATH is omitted, logs go to ` + defaultLogFile() + `
`

// debugFlag implements flag.Value + IsBoolFlag to support --debug (no value →
// default log file) and --debug=PATH. The stdlib flag package calls Set("true")
// for bare --debug when IsBoolFlag() returns true.
type debugFlag struct {
	enabled bool
	path    string // "" means "use defaultLogFile()"
}

func (d *debugFlag) String() string { return d.path }

func (d *debugFlag) Set(s string) error {
	d.enabled = true
	if s != "true" {
		d.path = s
	}
	return nil
}

func (d *debugFlag) IsBoolFlag() bool { return true }

// runInit implements `zjump init <shell>`. Only zsh and bash are supported
// (R-INIT-1); any other shell is rejected. Mirrors cmd/init.rs.
func runInit(args []string) error {
	fs := newFlagSet("init")
	var noCmd bool
	fs.BoolVar(&noCmd, "no-cmd", false, "")
	fs.BoolVar(&noCmd, "no-aliases", false, "")
	var cmd string
	fs.StringVar(&cmd, "cmd", "zz", "")
	var hook string
	fs.StringVar(&hook, "hook", "pwd", "")
	var dbg debugFlag
	fs.Var(&dbg, "debug", "enable debug logging (optional =PATH)")

	rest, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			printCmdHelp(os.Stdout, "init", initHelp)
			return nil
		}
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("init: exactly one shell argument is required (bash or zsh)")
	}
	sh := rest[0]
	if !shell.Supported(sh) {
		return fmt.Errorf("unsupported shell: %s (supported: bash, zsh)", sh)
	}
	switch hook {
	case "none", "prompt", "pwd":
	default:
		return fmt.Errorf("invalid hook: %s (choose none, prompt, or pwd)", hook)
	}

	opts := shell.Opts{
		Cmd:             cmd,
		HasCmd:          !noCmd,
		Hook:            hook,
		Echo:            config.Echo(),
		ResolveSymlinks: config.ResolveSymlinks(),
	}

	if dbg.enabled {
		logFile := dbg.path
		if logFile == "" {
			logFile = defaultLogFile()
		}
		absPath, err := filepath.Abs(logFile)
		if err != nil {
			return fmt.Errorf("init --debug: could not resolve path %q: %w", logFile, err)
		}
		// H1: the path is baked verbatim into the eval'd shell template
		// inside double quotes, so reject shell-unsafe characters rather
		// than trying to escape them.
		if strings.ContainsAny(absPath, "\"'`\\$") || strings.Contains(absPath, "\n") {
			return fmt.Errorf("init --debug: log file path contains a shell-unsafe character")
		}
		opts.Debug = true
		opts.DebugLogFile = absPath
	}

	src, err := shell.Render(sh, opts)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintln(os.Stdout, src)
	return errs.PipeExit(werr, "stdout")
}
