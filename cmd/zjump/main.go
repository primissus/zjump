// Command zjump is a frecency-based cd replacement — a Go reimplementation of
// zoxide (see ARCHITECTURE.md / DESIGN.md). This entrypoint parses and
// dispatches, then maps errors to exit codes: a SilentExit exits with its code
// and prints nothing; any other error prints as "zjump: <full causal chain>".
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"zjump/internal/cli"
	"zjump/internal/errs"
)

func main() {
	// Ignore SIGPIPE so a broken stdout pipe (e.g. `zjump query --list | head`)
	// surfaces as an EPIPE write error we can convert to a silent exit 0,
	// instead of the Go runtime killing us with signal 13 (R-ERR-1, A-7).
	signal.Ignore(syscall.SIGPIPE)

	if err := cli.Run(os.Args[1:]); err != nil {
		if se, ok := errs.AsSilentExit(err); ok {
			os.Exit(se.Code)
		}
		// Go's %v prints the full wrapped-error chain on one line — the causal
		// chain zoxide gets from anyhow's Debug formatting (R-ERR-3). No stack
		// trace is ever emitted.
		fmt.Fprintf(os.Stderr, "zjump: %v\n", err)
		os.Exit(1)
	}
}
