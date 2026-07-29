# Architecture

This document describes how zjump is built — the package layout, the on-disk
data formats, the frecency engine, the shell-integration model, and the
extension features (aliases, git branch/worktree jumps, `list`). It is aimed at
contributors who want to understand the codebase before changing it.

For the upstream zoxide behavior this project mirrors, see
[`ARCHITECTURE.md`](../ARCHITECTURE.md) (zoxide's Rust internals, as reference)
and [`DESIGN.md`](../DESIGN.md) (zoxide's CLI surface). This document describes
**zjump's own Go implementation**.

---

## 1. Overview

zjump is a single, short-lived Go binary. There is no daemon and no background
process. Every invocation — `zjump add`, `zjump query`, `zz foo`, … — opens a
local database file, does one unit of work, and (only when something changed)
atomically rewrites the file before exiting.

```
                 Shell (bash / zsh)
                        │
        ┌───────────────┼───────────────┐
        │               │               │
   zz / zzi          cd hook        Space-Tab
   (jump)            (track)       (complete)
        │               │               │
        ▼               ▼               │
  zjump query      zjump add            │
  (subprocess)     (subprocess)         │
        │               │               │
        ▼               ▼               │
  ┌─────────────────────────┐           │
  │   db  (ZJDB binary)      │           │
  │   aliases (ZJAL binary)  │◄──────────┘
  │   atomic.Write()         │
  └─────────────────────────┘
```

The "always running" part is the shell-integration script produced by
`zjump init bash|zsh`. The binary itself never persists between invocations.

---

## 2. Package layout

```
cmd/zjump/main.go          # entry point: SIGPIPE handling, error→exit mapping
internal/
  cli/                     # subcommand dispatch + per-subcommand flag parsing & logic
  config/                  # _ZJUMP_* env vars + data-dir resolution
  db/                      # frecency database: binary format, mutators, match stream
  alias/                   # name→directory alias store (separate binary file)
  atomic/                  # crash-safe atomic file writer (temp + sync + rename)
  errs/                    # SilentExit sentinel, broken-pipe handling
  fzf/                     # fzf binary wrapper: protocol, args, exit-code mapping
  git/                     # git shell-out helpers (repo root, worktree list, branch)
  glob/                    # Rust glob-crate subset for _ZJUMP_EXCLUDE_DIRS
  log/                     # tiny mutex-guarded file logger
  paths/                   # path normalization (lexical + symlink-resolving) + clock
  shell/                   # go:embed text/template renderer for bash/zsh init scripts
```

The canonical version constant lives in `internal/cli/cli.go` (`const Version`).

There are **no third-party Go dependencies** — the entire project is the
standard library plus `text/template` (stdlib) and `go:embed` (stdlib).

---

## 3. Entry point (`cmd/zjump`)

`main()` does two things:

1. `signal.Ignore(syscall.SIGPIPE)` — so a broken stdout pipe
   (e.g. `zjump query --list | head`) surfaces as an EPIPE write error that is
   converted to a silent exit 0, instead of the Go runtime killing the process
   with signal 13.

2. Calls `cli.Run(os.Args[1:])` and maps the returned error:
   - If the error is a `errs.SilentExit`, exits with the embedded code and
     prints **nothing**.
   - Otherwise prints `zjump: <full wrapped causal chain>` to stderr and exits 1.
     Go's `%v` on a wrapped error naturally prints the full chain on one line,
     mirroring zoxide's `anyhow` Debug formatting. No stack trace is ever
     emitted.

---

## 4. CLI dispatch (`internal/cli`)

`cli.Run(args)` implements the subcommand tree on top of the stdlib `flag`
package plus a small hand-rolled dispatcher:

1. `extractGlobalFlags(args)` strips `--debug[=PATH]` and `--log-file=PATH`
   that appear **before** the subcommand token, sets up the file logger if
   requested, and returns the remaining args starting at the subcommand.

2. A `switch` dispatches the subcommand token:

   | Token | Handler | Purpose |
   |---|---|---|
   | `add` | `runAdd` | Add/increment directories |
   | `query` | `runQuery` | Search the database |
   | `remove` | `runRemove` | Delete entries |
   | `init` | `runInit` | Render shell-integration script |
   | `edit` | `runEdit` | fzf-driven database browser |
   | `alias` | `runAlias` | Manage named shortcuts |
   | `branch` | `runBranch` | Jump to a git branch's worktree |
   | `worktree` | `runWorktree` | Jump to a named worktree |
   | `list` | `runList` | Combined view (zjump-only) |
   | `help` / `-h` / `--help` | — | Print usage |
   | `version` / `-V` / `--version` | — | Print version |

### Flag parsing helpers

- `parseArgs(fs, args)` — the stdlib `flag` package stops at the first
  positional. This wrapper **permutes** args so `zjump query foo -i` works the
  same as `zjump query -i foo`. Everything after a literal `--` is treated as
  positionals only (never flags), matching the shell functions that always pass
  `-- "$@"`.
- `newFlagSet(name)` — `flag.NewFlagSet(name, flag.ContinueOnError)` with output
  set to `io.Discard` so parse errors propagate as returned errors, printed
  once by `main`, not by the flag package itself.

---

## 5. Frecency database (`internal/db`)

### Core types (`dir.go`)

```go
type Rank  = float64
type Epoch = uint64

type Dir struct {
    Path         string
    Rank         Rank
    LastAccessed Epoch
}
```

**Frecency scoring** (`Score(now)`) — a decaying score, not raw rank:

| Last accessed | Score |
|---|---|
| within the last hour | `rank × 4` |
| within the last day | `rank × 2` |
| within the last week | `rank × 0.5` |
| older | `rank × 0.25` |

So a directory visited twice today outranks one visited a hundred times last
year.

### Database struct (`db.go`)

```go
type Database struct {
    path  string
    dirs  []Dir
    dirty bool
}
```

Key mutators:

- `AddUpdate(path, by, now)` — increment rank **and** set `LastAccessed = now`;
  insert if absent. Used by `add`.
- `Add(path, by, now)` — increment rank, leave `LastAccessed`; insert if absent.
  Used by `edit increment/decrement`.
- `Remove(path) bool` — O(1) swap-remove (order-disturbing).
- `Age(maxAge)` — if total raw rank exceeds `maxAge` (default 10000), scale
  every rank by `0.9 × maxAge / total` (a deliberate undershoot), then drop
  entries whose scaled rank falls below 1.0. Keeps the database bounded and
  prunes long-neglected directories.
- `SortByScore(now)` / `SortByPath()` — stable sorts. **Never set `dirty`**
  (D-4: never rewrite the file just for ordering).

### Binary on-disk format (`format.go`) — deviation D-1

zjump uses a native, versioned, little-endian binary format. It is **not**
byte-compatible with zoxide's `db.zo` and can never be confused with it.

```
offset 0 :  4 bytes  magic "ZJDB"
offset 4 :  u32       version (== 1)
offset 8 :  u64       entry count
per entry:
    u64   path length
    ...   path bytes (UTF-8, no terminator)
    f64   rank (IEEE-754)
    u64   last_accessed (epoch seconds)
```

Guards: 32 MiB max file size, strict magic/version checks, per-entry bounds
checks. A truncated or corrupt file is a hard error — it never yields a
silently-wrong database.

### Candidate stream (`stream.go`)

```go
type Stream struct {
    db   *Database
    idx  int            // walks best-first (reverse order after SortByScore)
    opts StreamOptions
}
```

`NewStream(db, opts)` sorts the database ascending by score, then `Next()`
walks indices in reverse (best-first), applying filters as it goes:

```
Next() filter pipeline (in order):
  1. keywords   → right-to-left substring match (see §6)
  2. base-dir   → component-wise prefix match
  3. exclude    → lazy swap-remove on match (deletes from DB as a side effect)
  4. exists     → lazy swap-remove only when stale (dir.LastAccessed < ttl)
                  stale = not accessed in ~90 days
```

Excluded and stale-nonexistent entries are **deleted from the DB as a side
effect of iterating** — mirroring zoxide's lazy-deletion behavior.

### Keyword matching (`match.go`)

zoxide's exact right-to-left substring algorithm (`R-MATCH-1/2/3`):

- No keywords → every path matches.
- The **last** keyword must occur within the final path component (nothing after
  its match may contain a `/`).
- Each earlier keyword must appear strictly to the left of the previously
  matched span, in order — spans cannot overlap.

Example: `["foo", "o", "bar"]` does **not** match `/foo/bar`: after `bar` and
`o` consume overlapping text, there's no room left for `foo`.

---

## 6. Aliases (`internal/alias`)

Named shortcuts to directories, stored in a separate versioned binary file
alongside the database.

### Binary format — `aliases` file

```
offset 0 :  4 bytes  magic "ZJAL"
offset 4 :  u32       version (== 1)
offset 8 :  u64       entry count
per entry:
    u64   name length
    ...   name bytes (UTF-8)
    u64   path length
    ...   path bytes (UTF-8)
```

Same discipline as the DB: size guard, magic/version checks, per-entry bounds
checks.

### Alias store (`store.go`)

- `Get(name)` — exact case-sensitive match.
- `Match(name)` — exact match first, then **unique prefix fallback**: if exactly
  one alias name starts with the query, it's used; ambiguous prefixes return no
  match. Used by `query` and `zz` in default mode with a single keyword.
- `Set(name, path)` — no-op if identical; otherwise updates and marks dirty.
- `Delete(name) bool` — removes from the map; marks dirty if present.
- `ValidateName(name)` — non-empty, no `/`, no `\n`/`\r`, not `.`/`..`, not
  starting with `-`.

When `zz <name>` matches an alias in default mode (single keyword), the alias
target is used directly — no frecency lookup. `--list`/`--interactive` bypass
alias resolution.

---

## 7. Atomic writes (`internal/atomic`)

```go
func Write(path string, data []byte) error
```

Crash-safe write semantics:

1. Create a temp file in the **same directory** as the target (so the rename is
   atomic on the same filesystem).
2. Write data.
3. `preserveOwner` — best-effort re-apply the existing target file's `uid`/`gid`
   on Unix (so an atomic rewrite doesn't silently change ownership).
4. `fsync` the temp file.
5. `os.Rename(temp, target)`.

A crash mid-write can never leave the target file truncated or corrupt. Used by
both `db.Database.Save` and `alias.Store.Save`.

---

## 8. Shell integration (`internal/shell`)

### Template rendering

```go
//go:embed templates/*.tmpl
var templatesFS embed.FS
```

`zjump init bash|zsh` renders a `text/template` embedded at compile time
(the Go analogue of zoxide's compile-time Askama templates). The `Opts` struct
configures the generated script:

| Field | Source | Effect |
|---|---|---|
| `Cmd` | `--cmd` (default `zz`) | Names the user-facing commands |
| `HasCmd` | `!--no-cmd` | Whether to define `zz`/`zzi` |
| `Hook` | `--hook` (default `pwd`) | `none` / `prompt` / `pwd` |
| `Echo` | `_ZJUMP_ECHO` | Print the matched dir before cd |
| `ResolveSymlinks` | `_ZJUMP_RESOLVE_SYMLINKS` | `pwd -P` vs `pwd -L` |
| `Debug` | `--debug[=PATH]` | Bake debug logging into the script |
| `DebugLogFile` | `--debug=PATH` | Absolute log path for spawned invocations |

### What the templates define

Both `bash.tmpl` and `zsh.tmpl` share the same structure:

- `__zjump_pwd` — `pwd -P` if `ResolveSymlinks`, else `pwd -L`.
- `__zjump_cd` — `cd -- "$@"` plus `&& __zjump_pwd` if `Echo`.
- `__zjump` — wrapper around `\command zjump`; adds `--debug --log-file` when
  `Debug` is set.
- `__zjump_hook` — fires `__zjump add -- "$(__zjump_pwd)"` on directory change
  or prompt. In bash `pwd` mode, additionally caches `__zjump_oldpwd` and only
  fires `add` when the directory actually changed.
- `__zjump_z` — the smart wrapper that powers `zz`: `--`/`-`/`..`/path → `cd`;
  `-a` → alias; `-b` → branch jump; `-w` → worktree jump; otherwise →
  `zjump query --exclude "$(__zjump_pwd)" -- "$@"`.
- `__zjump_zi` — `zjump query --interactive`.
- `zz` / `zzi` — user-facing commands (only when `HasCmd`).
- Completions (bash 4.4+, zsh with `zle`) — Space-Tab triggers interactive
  `query`, asynchronously rewrites the line.
- `__zjump_doctor` — warns if the hook is missing from `PROMPT_COMMAND` / hook
  arrays; suppressible via `_ZJUMP_DOCTOR=0`.

---

## 9. Extension: git branch & worktree jumps (`internal/git`)

```
type Worktree struct {
    Path     string
    Head     string
    Branch   string   // short name; empty when detached
    Detached bool
}
```

Wrapped git commands (via `os/exec`, no third-party dependency):

- `RepoRoot(dir)` — `git -C <dir> rev-parse --show-toplevel`.
- `Worktrees(dir)` — `git -C <dir> worktree list --porcelain`; parsed by a pure
  `parseWorktrees()` function tested independently of git.
- `CurrentBranch(dir)` — `git -C <dir> rev-parse --abbrev-ref HEAD`.

`zjump branch <name> [repo-keywords]`:

1. Resolve the repo: with no keywords, use CWD's `RepoRoot`; with keywords, query
   the frecency database to find the best-matching repo.
2. List worktrees; find the one whose `Branch` matches.
3. Print the path, or error (with available-branches hint).

`zjump worktree <name> [repo-keywords]`:

1. Same repo resolution.
2. Match by directory basename first (must be unique); then by branch shortname.
3. Print the path, or error (with available-worktrees hint).

Both support a no-arg interactive fzf picker. When CWD is not in a repo, the
fallback scans the top-N (`_ZJUMP_PICK_TOP`, default 10) database entries for
`.git` directories.

---

## 10. fzf wrapper (`internal/fzf`)

A builder + child-process wrapper around the external `fzf` binary:

```go
fzf, err := fzf.New()                 // exec.LookPath("fzf")
fzf.StdAppearance().EnablePreview()   // builder methods
child, err := fzf.Spawn()             // exec.Command, StdinPipe + StdoutPipe
child.Write("score\tpath")             // stream a candidate (NUL-terminated)
selection, err := child.Wait()        // maps exit code → error
```

Record protocol: each candidate is `"score\tpath\x00"` (NUL-terminated, tab for
`--delimiter`). The `--nth=2` flag makes fzf match only the path column.

Exit-code mapping:

| Code | Behavior |
|---|---|
| 0 | Success (selection in stdout) |
| 1 | `no match found` |
| 2 | `fzf returned an error` |
| 130 | `SilentExit{Code: 130}` — interactive cancellation, prints nothing |
| killed | `fzf was terminated` |

Used by: `query --interactive` (`zzi`), `edit`, and the no-arg
`branch`/`worktree` pickers.

fzf ≥ v0.51.0 is the documented minimum, but the version is **not enforced** at
runtime.

---

## 11. Path normalization (`internal/paths`)

Two deliberately non-interchangeable primitives:

- `ResolvePath(p)` — **pure lexical** normalization. No filesystem access, no
  symlink resolution. Relative paths joined onto `os.Getwd()`; `.` and `..`
  normalized as components; never pops past root. Used by `add` (lexical mode)
  and as `remove`'s fallback lookup.
- `Canonicalize(p)` — `filepath.Abs` + `filepath.EvalSymlinks`. **Requires the
  path to exist** (errors otherwise). Used by `add` when
  `_ZJUMP_RESOLVE_SYMLINKS=1`.

`CurrentTime()` — `time.Now().Unix()`, errors if the clock predates the Unix
epoch.

---

## 12. Glob matching (`internal/glob`)

A Go subset of the Rust `glob` crate's `Pattern::matches` with default
`MatchOptions` (`require_literal_separator = false`):

- `*` and `?` **do** match path separators (so `*` ≈ regex `.*`, `?` ≈ `.`).
- `**` as a whole path component is the globstar: matches zero or more
  components (`a/**/b` matches `a/b`).
- No backslash escaping (`\` is a literal).
- Whole-string anchored, case-sensitive.

Compiled to an anchored regexp at `New()` time. Used by
[`config.ExcludeDirs`](#config) and `db.Stream`'s exclude filter.

`Escape(s)` wraps metacharacters in single-char classes — used for the default
home-directory exclude pattern.

---

## 13. Configuration (`internal/config`)

| Function | Env var | Default | Notes |
|---|---|---|---|
| `DataDir()` | `_ZJUMP_DATA_DIR` | `~/Library/Application Support/zjump` (macOS), `~/.local/share/zjump` (Linux) | Must be absolute. D-2. |
| `Echo()` | `_ZJUMP_ECHO` | false | True only when value is `"1"`. |
| `ResolveSymlinks()` | `_ZJUMP_RESOLVE_SYMLINKS` | false | True only when value is `"1"`. |
| `ExcludeDirs()` | `_ZJUMP_EXCLUDE_DIRS` | Home dir (literal) | `:`-separated glob list. Default is `glob.Escape(home)`. |
| `FzfOpts()` | `_ZJUMP_FZF_OPTS` | unset | Read only by `query -i`, never by `edit`. |
| `Maxage()` | `_ZJUMP_MAXAGE` | `10000` | Parsed as u32. Aging ceiling. |
| `PickTop()` | `_ZJUMP_PICK_TOP` | `10` | Must be > 0. DB fallback limit for branch/worktree pickers. |

---

## 14. Error conventions (`internal/errs`)

```go
type SilentExit struct{ Code int }
```

- `SilentExit` is raised on **broken pipe** (exit 0 — e.g.
  `zjump query --list | head`) and on **interactive-picker cancellation**
  (exit 130).
- `PipeExit(err, device)` wraps non-pipe I/O errors with a device label
  (`"stdout"`, `"fzf"`).
- `main` uses `errors.As` to find a `SilentExit` anywhere in the chain and
  exits with its code silently; any other error is printed as
  `zjump: <full causal chain>`.

---

## 15. The five deliberate deviations (D-1..D-5)

These are intentional design decisions, preserved in code comments and the
README:

| ID | Deviation | Rationale |
|---|---|---|
| **D-1** | Native on-disk DB format (`ZJDB` magic + version) | Not byte-compatible with zoxide's `db.zo`; cannot collide. |
| **D-2** | `_ZJUMP_*` env-var prefix | Mirrors zoxide's `_ZO_*` 1:1, but distinct. |
| **D-3** | `edit` re-sorts after every change | Fixes zoxide's swap-remove sort-staleness mid-session. |
| **D-4** | DB rewritten only when actually dirty | Sorts never set dirty; `query`/`list` calls to `Save()` are no-ops when clean. |
| **D-5** | No Windows `cygpath` handling | Unix-only scope. |

---

## 16. `zjump list` — the combined view

A zjump-only extension with no zoxide equivalent. It combines directories,
aliases, branches, and worktrees into a single output.

```
DIRECTORIES                    ALIASES                   BRANCHES
──────────────                 ──────────                ─────────
/tmp/foo                       proj    ~/src/proj        main  ~/src/repo
/opt/bar                                                 WORKTREES
                                                         ────────
                                                         api   ~/src/repo/api
```

Key behaviors:

- Bare `zjump list` ≈ `zjump query --list`: a single DIRECTORIES section,
  best-first.
- Empty requested sections print their header + a single `(none)` row.
- `--json` emits structured JSON; requested-but-empty sections serialize as
  `[]` (not `null`).
- `--all-repos` scans every DB entry for `.git` and enumerates worktrees across
  all distinct repos (dedup by main-checkout path).
- `list` follows D-4: the database file is rewritten **only when** lazy
  deletions during iteration actually dirtied it.

---

## 17. Data flow walkthrough

### `zz foo` (the common case)

```
shell: zz foo
  → __zjump_z foo
  → zjump query --exclude "$(__zjump_pwd)" -- foo  (subprocess)
      → opens db file
      → db.NewStream(db, opts.WithKeywords("foo").WithExclude(pwd))
      → stream.Next() → best match (applies filters, lazy-deletes stale entries)
      → prints path
  → __zjump_cd <path>
  → shell hook fires (pwd mode): __zjump add -- "$(__zjump_pwd)"  (subprocess)
      → opens db file
      → database.AddUpdate(<path>, 1.0, now)
      → database.Age(maxAge)   (only if dirty)
      → database.Save()       (no-op unless dirty)
```

### `zjump edit`

```
zjump edit
  → opens db file
  → SortByScore (no dirty)
  → Save() (no-op unless already dirty — D-4)
  → spawn fzf with fixed key bindings (ctrl-r/d/w/s) + always-on preview
  → stream all entries as "score\tpath\0" records to fzf stdin
  → user presses a key:
      ctrl-d (delete)    → zjump edit delete <path>  → marks dirty
      ctrl-w (increment) → zjump edit increment <path> → marks dirty
      ctrl-s (decrement) → zjump edit decrement <path> → marks dirty
      ctrl-r (reload)    → D-3: SortByScore, then re-dump (always re-sorted)
  → on exit: Save() (persists mutations; D-4 no-op if clean)
```

---

## 18. Testing

Two test tags:

| Tag | Command | What it runs |
|---|---|---|
| *(default)* | `go test ./...` | Dependency-free suite: pure parsing, format, matching, glob, config tests. |
| `shelltests` | `go test -tags shelltests ./...` | Adds real bash/zsh/fzf/git integration tests. Requires `bash`, `zsh`, `fzf`, and `git` on PATH. |

The integration tests (`test/shell_test.go`, `test/e2e_test.go`) spawn real
shells, source `zjump init`, run `zz`, and assert end-to-end behavior.