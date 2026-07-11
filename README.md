<!-- markdownlint-configure-file { "MD013": { "code_blocks": false, "tables": false }, "MD033": false, "MD041": false } -->

# zjump

zjump is a **smarter cd command**, a Go reimplementation of
[zoxide](https://github.com/ajeetdsouza/zoxide).

It remembers which directories you use most frequently, so you can "jump" to
them in just a few keystrokes.<br />
zjump works on **bash** and **zsh** (Linux and macOS).

[Getting started](#getting-started) •
[Installation](#installation) •
[Commands](#commands) •
[Configuration](#configuration) •
[How it works](#how-it-works)

> **Scope.** zjump implements broad behavioral parity with zoxide for the
> `add`, `query`, `remove`, `init`, and `edit` commands. It also adds
> **typed-entry** capabilities zoxide lacks (**D-6**): jumping between git
> **worktrees** and **repositories**, checking out **branches**, bulk
> **indexing** of repos, and user-named **aliases** — all opt-in and additive
> (see [Typed entries](#typed-entries-repos-worktrees-branches-aliases)). The
> `import` command, shells other than bash/zsh, and Windows are **out of
> scope**; the database is a zjump-native format, not byte-compatible with
> zoxide's `db.zo`. See
> [Deliberate deviations](#deliberate-deviations-from-zoxide).

## Getting started

```sh
z foo              # cd into the highest-ranked directory matching foo
z foo bar          # cd into the highest-ranked directory matching foo and bar
z foo /            # cd into a subdirectory starting with foo

z ~/foo            # z also works like a regular cd command
z foo/             # cd into a relative path
z ..               # cd one level up
z -                # cd into the previous directory

zi foo             # cd with interactive selection (using fzf)

z foo<SPACE><TAB>  # show interactive completions (bash 4.4+/zsh only)

zr backend         # cd into an indexed git repository matching backend
zw feature         # cd into a git worktree (across indexed repos) matching feature
zr                 # no keywords -> interactive picker (same for zw)
zb main            # switch the current repo to a branch matching main
```

The `z` command tracks directories as you visit them and ranks them by
**frecency** (frequency + recency), so the places you actually use bubble to the
top. Read more about the [matching](#matching) and [scoring](#frecency-scoring)
algorithms below.

## Installation

zjump can be installed in 3 steps.

### 1. Install the binary

zjump is distributed as source. You'll need **Go 1.23+** and a Unix system
(Linux or macOS). From the repository root:

```sh
# Build and install to /usr/local/bin (may require sudo):
scripts/install.sh

# ...or install to a directory of your choice (must be on your PATH):
scripts/install.sh ~/.local/bin
```

Or build the binary directly and place it on your `PATH` yourself:

```sh
go build -o zjump ./cmd/zjump
```

### 2. Set up zjump on your shell

Add zjump to the **end** of your shell config file.

<details>
<summary>Bash</summary>

> Add this to the <ins>**end**</ins> of `~/.bashrc`:
>
> ```sh
> eval "$(zjump init bash)"
> ```

</details>

<details>
<summary>Zsh</summary>

> Add this to the <ins>**end**</ins> of `~/.zshrc`:
>
> ```sh
> eval "$(zjump init zsh)"
> ```
>
> For Space-Tab completions to work, this must be added _after_ `compinit` is
> called.

</details>

Then restart your shell (or `source` the config file). zjump will start
tracking directories as you `cd` around.

### 3. Install fzf (optional)

[fzf](https://github.com/junegunn/fzf) is a command-line fuzzy finder, used by
zjump for interactive selection (`zi`, `zjump edit`) and Space-Tab completions.
Core `z` jumping works without it.

> **Note:** the minimum supported fzf version is **v0.51.0**.

## Commands

The `zjump` binary exposes eight subcommands. In everyday use you'll rarely call
them directly — the `z`/`zi` shell functions and the tracking hook do it for you
— but the full surface is documented here. The typed-entry commands (`index`,
`alias`, `branch`, and `query --type`) are described under
[Typed entries](#typed-entries-repos-worktrees-branches-aliases).

Global flags: `-h`/`--help`, `-V`/`--version`.

### `zjump add <paths>...`

Add directories to the database, or increment the rank of existing entries.
This is what the shell hook runs on every navigation.

| Flag | Description |
| --- | --- |
| `<paths>...` | One or more directories to add/increment (required). |
| `-s`, `--score <N>` | Amount to increment the rank by (float, default `1.0`). |

- On an existing entry: `rank += N` (floored at `0.0`) and `last_accessed` is set
  to now. On a new path: it's inserted with `rank = N`.
- Paths containing a newline/carriage-return, or matching a
  [`_ZJUMP_EXCLUDE_DIRS`](#environment-variables) glob, are silently skipped.
- A path that isn't a directory is an error (`not a directory: <path>`).
- After any change, the [aging pass](#aging) runs against `_ZJUMP_MAXAGE`.

### `zjump query [keywords]...`

Search the database and print matching directories. This is what `z`/`zi`
invoke under the hood.

| Flag | Description |
| --- | --- |
| `[keywords]...` | Substrings to match against paths ([matching rules](#matching)). |
| `-l`, `--list` | Print **every** match, one per line (best first), instead of just the top one. |
| `-i`, `--interactive` | Select a match interactively via fzf. Conflicts with `--list`. |
| `-s`, `--score` | Prefix each result with its decayed frecency score. |
| `-a`, `--all` | Include directories that no longer exist (disables the existence filter). |
| `--exclude <path>` | Skip this exact path in the results (never deletes it; used by `z` to exclude `$PWD`). |
| `--base-dir <path>` | Only return matches that are component-wise under this directory. |
| `--type <type>` | Restrict the candidate set: `dir`, `repo`, `worktree`, `alias`, or `any` (see [Typed entries](#typed-entries-repos-worktrees-branches-aliases)). Omitted = plain directories + repos, plus the alias fast path. |

- Default mode prints the single best match, or errors `no match found`.
- `zjump query --list | head` and other pipelines exit cleanly (silent, code 0)
  on a broken pipe.

### `zjump remove [paths]...`

Remove directories from the database. For each argument, zjump tries an exact
string match, then a lexically-resolved absolute-path match. A path that matches
neither is an error (`path not found in database: <path>`).

### `zjump init <bash|zsh>`

Print the shell integration script (the `z`/`zi` functions, the tracking hook,
and completions). You source its output, typically via
`eval "$(zjump init bash)"`. See [Configuration](#configuration) for its flags.

### `zjump edit`

Launch an fzf-driven browser over the database (sorted best-score-first) to
inspect and adjust entries. Key bindings:

| Key | Action |
| --- | --- |
| `ctrl-r` | Reload the list |
| `ctrl-d` | Delete the highlighted entry |
| `ctrl-w` | Increment its rank (`+1.0`) |
| `ctrl-s` | Decrement its rank (`-1.0`, floored at 0) |
| `tab` / `shift-tab` | Move down / up |
| `enter` / `esc` | Exit (no directory is selected) |

> `edit` re-sorts the list after every change, so the ordering never goes stale
> mid-session (see [deviations](#deliberate-deviations-from-zoxide)).

## Typed entries (repos, worktrees, branches, aliases)

Beyond plain directory jumping, zjump tracks **typed** entries and can jump
between them. This is a capability zoxide lacks (**D-6**); everything here is
additive and opt-in — with no `--type` and no new command, zjump behaves exactly
as it always has.

Each stored entry has a **kind**: a plain directory (`dir`), the root of a git
repository (`repo`), or a user-named alias (`alias`). Repo roots are detected
automatically — any path with a `.git` directory or file is stored as a repo
(and a plain-dir entry is upgraded in place the first time it's seen as one; a
repo is never downgraded). **Worktrees are never stored**: they're discovered
live at query time from the indexed repos.

### `zjump query --type <type>`

`--type` chooses what the query searches:

| Type | Candidates | Keywords match |
| --- | --- | --- |
| *(omitted)* | directories + repos (plus the alias fast path below) | path |
| `dir` | plain directories only | path |
| `repo` | repository roots only | path |
| `worktree` | live-enumerated worktrees of indexed repos | worktree path |
| `alias` | aliases (output is the target path) | alias **name** |
| `any` | every stored entry (no worktree enumeration) | path, or name for aliases |

Worktree enumeration streams your indexed repos best-frecency-first, runs
`git worktree list` on each (bare worktrees skipped, detached HEADs labelled),
de-duplicates shared paths, and probes at most the first 50 repos. No `git`
subprocess ever runs unless `--type worktree` is requested.

### `zjump index [--max-depth N] [--score S] <roots>...`

Bulk-scan one or more roots for git repositories and index them (as `repo`
entries). The walk records a repo root and does **not** descend into it (nested
repos/submodules are out of scope), skips hidden directories, never follows
symlinks, and descends at most `--max-depth` levels (default `3`; the root is
depth 0). Each repo is added with score `S` (default `1.0`), so re-running
refreshes frecency. A summary is printed to stderr.

### `zjump alias add|rm|list`

User-named shortcuts that resolve to paths.

```sh
zjump alias add work ~/src/project   # PATH defaults to the current directory
zjump alias rm work
zjump alias list [--score]           # name<TAB>path, best-score-first
```

- Names may not be empty, contain `/` or whitespace, start with `-`, or be the
  bare cd-idioms `.`, `..`, `-`, or `~`.
- Re-adding an existing name replaces its target in place, preserving rank.
- **Multiple aliases may point at the same target** — alias identity is by name,
  and a path can be a dir/repo entry and any number of alias targets at once.
- Aliases are exempt from aging/cleanup: they're removed **only** by `alias rm`,
  never by the aging pass, the existence filter, or `zjump remove`.

**Alias fast path.** In plain `z foo` usage (a single keyword, non-interactive),
zjump first checks aliases: an exact name match wins outright, otherwise the
best-scoring name-prefix alias. A hit bumps that alias's rank and jumps to its
target; otherwise the query falls through to normal directory matching. An alias
whose target equals the excluded `$PWD` is passed over.

### `zjump branch [pattern]`

List the current repository's local branches and print the chosen one (the shell
function runs the actual `git switch`). With a `pattern` that substring-matches
exactly one branch, it prints that branch directly — no fzf needed, so it's
scriptable. Otherwise it opens an fzf picker with the current branch marked and
moved to the top. Errors `not inside a git repository` / `no branches found`.

### Generated shell functions

Alongside `z`/`zi`, `zjump init` defines three more functions (renamed by
`--cmd`, all suppressed by `--no-cmd`):

| Function | Behavior |
| --- | --- |
| `zw` | Jump to a worktree (`query --type worktree`); no keywords → interactive. |
| `zr` | Jump to a repository (`query --type repo`); no keywords → interactive. |
| `zb` | `zjump branch` then `git switch` to the selection. |

`zw`/`zr` honor `_ZJUMP_ECHO` like `z`; cancelling the fzf picker (Ctrl-C) is
silent. There are no Space-Tab completions for these three functions.

## Configuration

### `init` flags

When calling `zjump init`, the following flags are available:

- **`--cmd <cmd>`**
  - Changes the prefix of the `z` and `zi` commands. Default: `z`.
  - `--cmd j` changes them to `j` / `ji`.
  - `--cmd cd` replaces the `cd` command.
- **`--hook <hook>`**
  - Changes how often zjump increments a directory's score:

    | Hook | Description |
    | --- | --- |
    | `none` | Never (only explicit `zjump add` grows the database) |
    | `prompt` | At every shell prompt |
    | `pwd` (default) | Whenever the working directory changes |

  - On bash there is no native "directory changed" event, so `pwd` mode is
    emulated: the hook runs at every prompt but only calls `zjump add` when the
    directory actually changed.
- **`--no-cmd`** (alias `--no-aliases`)
  - Prevents zjump from defining the `z`, `zi`, `zw`, `zr`, and `zb` commands.
    The underlying `z`/`zi` functions remain available as `__zjump_z` and
    `__zjump_zi` if you want to wire them up yourself.

### Environment variables

Environment variables configure zjump. Those consumed by the shell integration
(`_ZJUMP_ECHO`, `_ZJUMP_RESOLVE_SYMLINKS`) must be set **before** `zjump init` is
called; the rest are read on each invocation.

- **`_ZJUMP_DATA_DIR`**
  - Directory holding the database file (named `db`). Must be an absolute path.
  - Default:

    | OS | Path |
    | --- | --- |
    | Linux / BSD | `$XDG_DATA_HOME/zjump` or `$HOME/.local/share/zjump` |
    | macOS | `$HOME/Library/Application Support/zjump` |

- **`_ZJUMP_ECHO`**
  - When set to `1`, `z` prints the matched directory before navigating to it.
- **`_ZJUMP_EXCLUDE_DIRS`**
  - Directories to exclude from the database, as a `:`-separated list of
    [globs](https://man7.org/linux/man-pages/man7/glob.7.html) (e.g.
    `$HOME:$HOME/private/*`). `*`/`?` match across `/`; `**` matches any number
    of path components.
  - Excluded paths are skipped by `add` and purged from the database when
    encountered during `query`.
  - Default: `"$HOME"` (your home directory, matched literally — not its
    subdirectories).
- **`_ZJUMP_FZF_OPTS`**
  - Custom options passed to fzf during interactive selection (`query -i`). When
    set, it replaces zjump's built-in fzf arguments and disables the preview
    pane. Not read by `zjump edit`.
- **`_ZJUMP_MAXAGE`**
  - The [aging](#aging) ceiling: the total raw rank at which entries start being
    rescaled and pruned. Parsed as an unsigned integer. Default: `10000`.
- **`_ZJUMP_RESOLVE_SYMLINKS`**
  - When set to `1`, symlinks are resolved before directories are added.

## How it works

zjump is a stateless, short-lived binary. Every `z`/`add`/`query` invocation
opens a single database file, does one unit of work, and (only when something
changed) atomically rewrites it. There is no daemon — the shell hook simply runs
`zjump add` as you navigate.

### Matching

Keyword matching is **case-insensitive plain-substring** (not fuzzy or
edit-distance), evaluated right-to-left:

- With no keywords, every directory matches.
- The **last** keyword must occur within the final path component (nothing after
  its match may contain a `/`).
- Each earlier keyword must then appear, in order, strictly to the left of the
  previous match — keyword matches cannot overlap or appear out of order.

For example, `["foo", "o", "bar"]` does **not** match `/foo/bar`: after `bar`
and `o` consume overlapping text, there's no room left for `foo`.

### Frecency scoring

Each entry has a raw `rank` (a usage counter, `+1.0` per visit by default) and a
`last_accessed` timestamp. Queries don't sort by raw rank — they compute a
**decayed score**:

| Last accessed | Score |
| --- | --- |
| within the last hour | `rank × 4` |
| within the last day | `rank × 2` |
| within the last week | `rank × 0.5` |
| older | `rank × 0.25` |

The highest-scoring match wins, so a directory you visit constantly today
outranks one you visited a hundred times last year.

### Aging

To keep the database bounded, whenever the sum of every entry's raw rank exceeds
`_ZJUMP_MAXAGE` (default `10000`), every rank is scaled by
`0.9 × maxage / total`, and entries whose rank then falls below `1.0` are
removed. This prunes long-neglected, low-weight directories over time.

### Existence & cleanup

By default `query` only returns directories that still exist. An entry whose
directory is gone is hidden, and permanently removed once it's also stale
(not accessed in ~90 days) — so an unmounted drive is hidden but kept, while a
truly-deleted directory eventually disappears. `--all` disables this.

## Deliberate deviations from zoxide

zjump targets broad behavioral parity, not bug-for-bug parity. The intentional
differences:

- **D-1** — a zjump-native, versioned binary database (magic `ZJDB` + a version
  header, 32 MiB read guard) under a zjump-specific data directory and filename.
  It is **not** byte-compatible with zoxide's `db.zo` and can never be confused
  with it. The current format is **version 2** (it adds per-entry kind + alias
  name). A version-1 database still loads, and is transparently rewritten as
  version 2 on the next change that modifies it — a **one-way** upgrade with no
  downgrade path.
- **D-2** — the `_ZJUMP_*` environment-variable prefix (vs. zoxide's `_ZO_*`).
- **D-3** — `edit` re-sorts after every change, fixing zoxide's session-scoped
  sort-staleness quirk.
- **D-4** — `query` rewrites the database **only when it actually changed** (a
  lazy deletion), rather than on every invocation.
- **D-5** — no Windows `cygpath` handling; zjump is Unix-only.
- **D-6** — zjump **adds** capabilities zoxide has no equivalent for: typed
  entries with `query --type`, repository indexing (`index`), user-named aliases
  (`alias`), branch checkout (`branch`), live worktree jumps, and the
  `zw`/`zr`/`zb` shell functions. All of it is additive and opt-in; the original
  zoxide-parity surface is unchanged. See
  [Typed entries](#typed-entries-repos-worktrees-branches-aliases).

Also out of scope: the `import` subcommand, shells other than bash/zsh, and
edit-distance matching.

## Development

```sh
go build ./...                    # build everything
go test ./...                     # fast, dependency-free test suite
go test -tags shelltests ./...    # also run real bash/zsh/fzf integration tests
```

See [`REQUIREMENTS.md`](./REQUIREMENTS.md) for the scope contract (stable `R-*`
IDs), [`PLAN.md`](./PLAN.md) for the roadmap, and
[`ARCHITECTURE.md`](./ARCHITECTURE.md) / [`DESIGN.md`](./DESIGN.md) for the
zoxide reference material zjump is built against.

## Credit

zjump is a from-scratch Go reimplementation inspired by, and designed to be
behaviorally compatible with, [zoxide](https://github.com/ajeetdsouza/zoxide) by
Ajeet D'Souza. All credit for the original design and algorithm goes to that
project; zjump is not affiliated with it.
