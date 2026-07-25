// Package shell renders zjump's shell-integration scripts. Each supported shell
// has a go:embed'd text/template rendered from an Opts struct — the Go analogue
// of zoxide's compile-time Askama templates (ARCHITECTURE.md §7, §12). Only zsh
// and bash are in scope (R-INIT-1); Windows cygpath handling is omitted (D-5).
package shell

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

// Opts are the init-time options baked into a rendered script.
type Opts struct {
	Cmd             string // jump-command prefix (e.g. "zz"); used only when HasCmd
	HasCmd          bool   // false under --no-cmd
	Hook            string // "none" | "prompt" | "pwd"
	Echo            bool   // from _ZJUMP_ECHO
	ResolveSymlinks bool   // from _ZJUMP_RESOLVE_SYMLINKS
}

var tmpl = template.Must(template.New("shell").ParseFS(templatesFS, "templates/*.tmpl"))

// Supported reports whether shell has an integration template.
func Supported(shell string) bool {
	return shell == "zsh" || shell == "bash"
}

// Render produces the integration script for the named shell.
func Render(shell string, opts Opts) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, shell+".tmpl", opts); err != nil {
		return "", fmt.Errorf("could not render template: %w", err)
	}
	return buf.String(), nil
}
