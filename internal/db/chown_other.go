//go:build !unix

package db

import "os"

// preserveOwner is a no-op off Unix. zjump targets Unix only (N-4); this stub
// exists solely so cross-GOOS `go build` stays clean.
func preserveOwner(f *os.File, path string) {}
