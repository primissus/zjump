package cli

import (
	"fmt"
	"os"

	"zjump/internal/config"
	"zjump/internal/errs"
	"zjump/internal/shell"
)

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

	rest, err := parseArgs(fs, args)
	if err != nil {
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
	src, err := shell.Render(sh, opts)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintln(os.Stdout, src)
	return errs.PipeExit(werr, "stdout")
}
