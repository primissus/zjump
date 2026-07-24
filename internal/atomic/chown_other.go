//go:build !unix

package atomic

import "os"

// preserveOwner is a no-op off Unix.
func preserveOwner(f *os.File, path string) {}
