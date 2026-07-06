// Package errs holds zjump's process-level error conventions: the SilentExit
// sentinel, broken-pipe tolerance, and the causal-chain printing contract
// (ARCHITECTURE.md §6, REQUIREMENTS.md §2.10).
package errs

import (
	"errors"
	"io"
	"syscall"
)

// SilentExit signals "stop quietly with this exit code, print nothing". Raised
// on a broken pipe (code 0) and on interactive-picker cancellation (code 130).
// Its Error() is intentionally empty (R-ERR-1/2).
type SilentExit struct {
	Code int
}

func (e SilentExit) Error() string { return "" }

// AsSilentExit extracts a SilentExit from anywhere in the error chain.
func AsSilentExit(err error) (SilentExit, bool) {
	var se SilentExit
	if errors.As(err, &se) {
		return se, true
	}
	return SilentExit{}, false
}

// IsBrokenPipe reports whether err is (or wraps) a broken-pipe write error.
func IsBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, io.ErrClosedPipe)
}

// PipeExit maps a write error to zjump's silent-exit contract: a broken pipe
// becomes a silent exit 0; any other I/O error is wrapped with device context;
// nil stays nil (R-ERR-1). Mirrors BrokenPipeHandler::pipe_exit.
func PipeExit(err error, device string) error {
	if err == nil {
		return nil
	}
	if IsBrokenPipe(err) {
		return SilentExit{Code: 0}
	}
	return &writeError{device: device, err: err}
}

type writeError struct {
	device string
	err    error
}

func (w *writeError) Error() string { return "could not write to " + w.device + ": " + w.err.Error() }
func (w *writeError) Unwrap() error { return w.err }
