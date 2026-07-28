// Package log provides a minimal debug logger gated behind the --debug flag.
// When enabled, timestamped messages are written to the file specified by
// --log-file (default: os.TempDir()/zjump-debug.log). All writes are
// synchronized and fail silently to avoid disturbing the primary command flow.
package log

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

var (
	mu     sync.Mutex
	w      io.WriteCloser
	enable bool
)

// Setup opens path for append and enables debug logging. Only the first call
// has effect; subsequent calls are ignored. Returns nil on success or the
// underlying file error.
func Setup(path string) error {
	mu.Lock()
	defer mu.Unlock()
	if enable {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	w = f
	enable = true
	return nil
}

// Enabled reports whether debug logging is active.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enable
}

// Debugf writes a timestamped line if debug logging is enabled. Errors are
// discarded (log failures must not disturb the primary command).
func Debugf(format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()
	if !enable || w == nil {
		return
	}
	now := time.Now().Format("2006-01-02T15:04:05.000")
	fmt.Fprintf(w, "%s ", now)
	fmt.Fprintf(w, format, args...)
	if len(format) == 0 || format[len(format)-1] != '\n' {
		fmt.Fprint(w, "\n")
	}
}

// Close flushes and closes the underlying file. Safe to call when logging was
// never enabled.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if w != nil {
		w.Close()
		w = nil
	}
	enable = false
}
