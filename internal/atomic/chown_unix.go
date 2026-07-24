//go:build unix

package atomic

import (
	"os"
	"syscall"
)

// preserveOwner best-effort re-applies the target file's existing uid/gid to the
// freshly-written temp file, so an atomic rewrite doesn't silently change
// ownership (e.g. a root-run save over a user-owned file). Failures are ignored.
func preserveOwner(f *os.File, path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	_ = f.Chown(int(st.Uid), int(st.Gid))
}
