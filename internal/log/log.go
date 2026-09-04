package log

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Level int8

const (
	Disabled Level = -1
	Debug    Level = 0
	Info     Level = 1
	Warn     Level = 2
	Error    Level = 3
)

var (
	mu    sync.Mutex
	w     io.WriteCloser
	level Level = Disabled
)

func Setup(path string) error {
	return SetupLevel(path, Debug)
}

func SetupLevel(path string, lvl Level) error {
	mu.Lock()
	defer mu.Unlock()
	// M6: re-setup is explicit — close the previous file instead of
	// silently keeping the old path/level while the caller believes the
	// new one took effect.
	if level != Disabled {
		if w != nil {
			w.Close()
			w = nil
		}
		level = Disabled
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	w = f
	level = lvl
	return nil
}

func SetLevel(lvl Level) {
	mu.Lock()
	defer mu.Unlock()
	level = lvl
}

func Enabled() bool {
	return EnabledAt(Debug)
}

func EnabledAt(lvl Level) bool {
	mu.Lock()
	defer mu.Unlock()
	return level != Disabled && lvl >= level
}

func Debugf(format string, args ...interface{}) {
	logf(Debug, "DEBUG", format, args...)
}

func Infof(format string, args ...interface{}) {
	logf(Info, "INFO", format, args...)
}

func Warnf(format string, args ...interface{}) {
	logf(Warn, "WARN", format, args...)
}

func Errorf(format string, args ...interface{}) {
	logf(Error, "ERROR", format, args...)
}

func logf(lvl Level, label, format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()
	if level == Disabled || lvl < level || w == nil {
		return
	}
	now := time.Now().Format("2006-01-02T15:04:05.000")
	fmt.Fprintf(w, "%s [%s] ", now, label)
	fmt.Fprintf(w, format, args...)
	if len(format) == 0 || format[len(format)-1] != '\n' {
		fmt.Fprint(w, "\n")
	}
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if w != nil {
		w.Close()
		w = nil
	}
	level = Disabled
}
