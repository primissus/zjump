# Development

This guide covers everything you need to set up a local development environment,
build, test, and contribute to zjump.

---

## Prerequisites

| Tool | Version | Required for |
|---|---|---|
| **Go** | 1.23 or later | Building, unit tests |
| **bash** | 4.0+ | Shell-integration tests (`shelltests` tag) |
| **zsh** | any | Shell-integration tests (`shelltests` tag) |
| **fzf** | v0.51.0+ | Interactive-selection tests (`shelltests` tag) |
| **git** | any | Branch/worktree integration tests (`shelltests` tag) |
| **make** | any | Convenience targets (optional) |

You only need Go for the default test suite. The integration tests additionally
require bash, zsh, fzf, and git on your `PATH`.

---

## Getting started

```sh
# Clone the repository
git clone https://github.com/primissus/zjump.git
cd zjump

# Build the binary to the repo root
make build

# Or build to bin/
make dev

# Run the fast, dependency-free test suite
make test

# Run all tests including shell/git integration
make test-all
```

---

## Building

```sh
# Build binary to repo root (./zjump)
make build

# Build to bin/zjump
make dev

# Run the built binary
make run ARGS="init bash"

# Or build directly with Go
go build -o zjump ./cmd/zjump
```

---

## Testing

zjump has two test tiers:

### Default suite (no shell/fzf/git required)

```sh
go test ./...
# or
make test
```

Runs pure-Go unit tests: database format serialization, frecency scoring,
keyword matching, glob compilation, config parsing, path normalization, fzf
record protocol, git-oriented porcelain parser, alias store, atomic-writer
behavior, and CLI flag permutations.

### Integration suite (requires bash, zsh, fzf, git)

```sh
go test -tags shelltests ./...
# or
make test-all
```

Adds real shell-integration tests (`test/shell_test.go`) that spawn actual
bash/zsh processes, source `zjump init bash|zsh`, exercise `zz`/`zzi`, and
verify end-to-end behavior. Also includes git branch/worktree integration tests
(`test/e2e_test.go`) that create real temporary git repositories.

### Running a specific test

```sh
go test ./internal/db/ -run TestScore
go test -tags shelltests ./test/ -run TestZsh
```

### Test conventions

- Tests live alongside the code they test, in `_test.go` files within each
  package.
- The `test/` directory at the repo root holds shell and e2e integration tests
  gated behind the `shelltests` build tag.
- [`test/TEST-CASES.md`](../test/TEST-CASES.md) documents the test matrix.

---

## Linting & formatting

```sh
# Check formatting and vet
make lint

# Auto-format all Go source
make fmt

# Equivalent commands:
gofmt -l .
go vet ./...
```

There is no separate linter dependency — `gofmt` and `go vet` from the standard
toolchain are all you need.

---

## Project layout

```
cmd/zjump/         # Main package — entry point only
internal/
  cli/             # Subcommand dispatch + flag parsing + per-command logic
  config/          # _ZJUMP_* env vars + data-dir resolution
  db/              # Frecency database (binary format, mutators, match stream)
  alias/           # Named-directory alias store
  atomic/          # Crash-safe atomic file writer
  errs/             # SilentExit, broken-pipe handling
  fzf/              # fzf binary wrapper
  git/              # git shell-out helpers
  glob/             # Rust glob-crate subset for _ZJUMP_EXCLUDE_DIRS
  log/              # Minimal file logger
  paths/            # Path normalization + clock
  shell/            # Shell-integration template renderer (go:embed)
    templates/      # bash.tmpl, zsh.tmpl
test/               # Integration tests (shelltests build tag)
scripts/           # install.sh
docs/              # This file + architecture.md
```

See [`architecture.md`](./architecture.md) for a deep dive into each package.

---

## Code conventions

- **Idiomatic Go.** Follow standard Go project layout conventions (`cmd/`,
  `internal/`).
- **Standard library first.** There are zero third-party dependencies. The
  entire project is the standard library plus `text/template` and `go:embed`
  (both stdlib). Justify any new dependency against the alternatives noted in
  `ARCHITECTURE.md §12`.
- **Tests alongside code.** Write tests in the same package, in `_test.go`
  files, not as a follow-up.
- **D-n deviations.** The five deliberate deviations from zoxide (D-1..D-5) are
  intentional. Every one carries its `D-n` ID in a code comment. Keep them, and
  keep the comments.
- **CLI parity.** Keep behavior compatible with the parity table in
  `DESIGN.md §12`. Any deliberate deviation must be called out and justified.

---

## Scope discipline

The build scope is defined in [`REQUIREMENTS.md`](../REQUIREMENTS.md) and
tracked in [`PLAN.md`](../PLAN.md). The out-of-scope items remain out unless
explicitly re-scoped:

| ID | Out of scope |
|---|---|
| N-1 | `import` subcommand |
| N-2 | Shells other than bash/zsh (fish, nushell, etc.) |
| N-3 | Byte-compatibility with zoxide's `db.zo` |
| N-4 | Windows support |
| N-5 | Edit-distance (Levenshtein) matching |

If you want to work on something outside this scope, open an issue first to
discuss it.

---

## Debugging

zjump has a built-in debug logger. You can enable it at `init` time to bake
logging into the generated shell script:

```sh
# Bake debug logging into the shell script
eval "$(zjump init bash --debug)"

# Or specify a custom log path
eval "$(zjump init bash --debug=/tmp/zjump-debug.log)"
```

Every `zz` and `zjump` subprocess invocation will write timestamped log lines
(including errors) to the log file. This is a zjump-only addition beyond zoxide
parity.

---

## Versioning & releases

The canonical version lives in `internal/cli/cli.go` (`const Version`).

Release process:

1. Update `const Version` in `internal/cli/cli.go`.
2. Commit: `chore: bump version to <version>`.
3. Tag: `git tag -a v<version> -m "v<version>"`.
4. Push the tag: `git push origin v<version>`.

The tag triggers `.github/workflows/release.yml`, which runs goreleaser to
cross-compile (Linux + macOS, amd64 + arm64) and publish GitHub Release
assets.

goreleaser reads the version from the git tag, not the Go constant. Keep both
in sync.

---

## Reference documents

| Document | Purpose |
|---|---|
| [`architecture.md`](./architecture.md) | zjump's own Go implementation in detail |
| [`../ARCHITECTURE.md`](../ARCHITECTURE.md) | Upstream zoxide (Rust) architecture — reference material |
| [`../DESIGN.md`](../DESIGN.md) | Upstream zoxide CLI surface — parity reference |
| [`../REQUIREMENTS.md`](../REQUIREMENTS.md) | Scope contract (stable `R-*` IDs) |
| [`../PLAN.md`](../PLAN.md) | Roadmap |
| [`../AGENTS.md`](../AGENTS.md) | Agent instructions (for AI-assisted development) |