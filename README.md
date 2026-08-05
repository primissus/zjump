<!-- markdownlint-configure-file { "MD013": { "code_blocks": false, "tables": false }, "MD033": false, "MD041": false } -->

# zjump

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![GitHub Release](https://img.shields.io/github/v/release/primissus/zjump)](https://github.com/primissus/zjump/releases)

zjump is a **smarter cd command**, a Go reimplementation of
[zoxide](https://github.com/ajeetdsouza/zoxide).

It remembers which directories you use most frequently, so you can "jump" to
them in just a few keystrokes.<br />
zjump works on **bash** and **zsh** (Linux and macOS).

[Getting started](#getting-started) •
[Installation](#installation) •
[Commands](#commands) •
[Configuration](#configuration) •
[How it works](#how-it-works) •
[Development](#development) •
[Contributing](#contributing)

> **Scope.** zjump implements broad behavioral parity with zoxide for the
> `add`, `query`, `remove`, `init`, and `edit` commands. It also extends beyond
> zoxide with directory aliases, git branch jumps, git worktree jumps, and a
> combined `list` view of all three. The `import` command, shells other than
> bash/zsh, and Windows are **out of scope**; the database is a zjump-native
> format, not byte-compatible with zoxide's `db.zo`. See
> [Deliberate deviations](#deliberate-deviations-from-zoxide).

## Getting started

```sh
zz foo              # cd into the highest-ranked directory matching foo
zz foo bar          # cd into the highest-ranked directory matching foo and bar
zz foo /            # cd into a subdirectory starting with foo

zz ~/foo            # zz also works like a regular cd command
zz foo/             # cd into a relative path
zz ..               # cd one level up
zz -                # cd into the previous directory

zzi foo             # cd with interactive selection (using fzf)

zz foo<SPACE><TAB>  # show interactive completions (bash 4.4+/zsh only)

zz -a proj ~/src/proj  # create an alias 'proj' → ~/src/proj
zz proj                # jump to the alias target (aliases beat frecency)
zz -b main myrepo      # jump to the worktree where 'main' is checked out
zz -w api myrepo       # jump to a worktree by name (e.g. directory named 'api')
zz -W                  # fzf-pick a worktree across every repo in the DB
```

The `zz` command tracks directories as you visit them and ranks them by
**frecency** (frequency + recency), so the places you actually use bubble to the
top. Read more about the [matching](#matching) and [scoring](#frecency-scoring)
algorithms below.

## Installation

zjump can be installed in 3 steps.

### 1. Install the binary

**Quick install** (downloads the latest release):

```sh
curl -fsSL https://raw.githubusercontent.com/primissus/zjump/main/scripts/install.sh | bash
```

**From source** (requires Go 1.23+):

```sh
# Build and install to /usr/local/bin (may require sudo):
scripts/install.sh --build

# ...or install to a directory of your choice (must be on your PATH):
scripts/install.sh --build ~/.local/bin
```

Or build the binary directly and place it on your `PATH` yourself:

```sh
# From source:
go build -o zjump ./cmd/zjump

# Or install via Go toolchain:
go install github.com/primissus/zjump/cmd/zjump@latest
```

Pre-built binaries for Linux and macOS (amd64, arm64) are available on the
[GitHub Releases](https://github.com/primissus/zjump/releases) page.

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
zjump for interactive selection (`zzi`, `zjump edit`) and Space-Tab completions.
Core `zz` jumping works without it.

> **Note:** the minimum supported fzf version is **v0.51.0**.

## Commands

The `zjump` binary exposes nine subcommands. In everyday use you'll rarely call
them directly — the `zz`/`zzi` shell functions and the tracking hook do it for you
— but the full surface is documented here.

Global flags: `-h`/`--help`, `-V`/`--version`, plus `--debug` (enable debug
logging) and `--log-file <PATH>` (log destination; default
`/tmp/zjump-debug.log`). The pseudo-subcommands `zjump help` and `zjump version`
are aliases for `-h`/`--help` and `-V`/`--version`. Every subcommand also
accepts `-h`/`--help` and prints its own usage with flags.

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

Search the database and print matching directories. This is what `zz`/`zzi`
invoke under the hood.

| Flag | Description |
| --- | --- |
| `[keywords]...` | Substrings to match against paths ([matching rules](#matching)). |
| `-l`, `--list` | Print **every** match, one per line (best first), instead of just the top one. |
| `-i`, `--interactive` | Select a match interactively via fzf. Conflicts with `--list`. |
| `-s`, `--score` | Prefix each result with its decayed frecency score. |
| `-a`, `--all` | Include directories that no longer exist (disables the existence filter). |
| `--exclude <path>` | Skip this exact path in the results (never deletes it; used by `zz` to exclude `$PWD`). |
| `--base-dir <path>` | Only return matches that are component-wise under this directory. |

- Default mode prints the single best match, or errors `no match found`.
- `zjump query --list | head` and other pipelines exit cleanly (silent, code 0)
  on a broken pipe.

### `zjump remove [paths]...`

Remove directories from the database. For each argument, zjump tries an exact
string match, then a lexically-resolved absolute-path match. A path that matches
neither is an error (`path not found in database: <path>`).

### `zjump init <bash|zsh>`

Print the shell integration script (the `zz`/`zzi` functions, the tracking hook,
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

### `zjump alias [<name> <dir>]`

Manage named directory shortcuts (aliases) that live outside the frecency
database.

- `zjump alias` — list all aliases (one per line, `name<TAB>path`, sorted by name).
- `zjump alias <name> <dir>` — create or overwrite an alias. `dir` must be an
  existing directory. Names cannot contain `/` or start with `-`.
- `zjump alias -d|--delete <name>` — remove an alias.

Aliases are stored in `<data-dir>/aliases` — a separate versioned binary file
with the same crash-safe atomic writes as the database.

When `z <name>` matches an alias (case-sensitive, single keyword, default mode),
the alias target is used directly — no frecency lookup. Multi-keyword queries
and `--list`/`--interactive` modes bypass alias resolution.

### `zjump branch <branch> [repo-keywords...]`

Print the worktree path (including the main checkout) where `<branch>` is
checked out. When `[repo-keywords]` are given, they are matched against the
frecency database to locate the repository; otherwise the current working
directory's repository is used. Never creates worktrees — read-only.

### `zjump worktree <name> [repo-keywords...]`

Print a worktree path matching `<name>` by directory basename first, then by
branch shortname. Multiple matches produce an ambiguity error. Repository
resolution works the same as `branch`.

Every `worktree`/`branch` lookup also **seeds** the resolved repository's
worktree paths into the frecency database (once each, rank 1.0, via
`database.Add` when not already present), so a first `zz -w`/`zz -b` makes all
of a repo's worktrees jumpable by plain `zz <keyword>` afterward.

### `zjump worktree --all`

List worktrees across **every** repository known to the frecency database,
launching the same interactive fzf picker as bare `zz -w` but ignoring the
current directory (so it works even when you're inside a repo). Repos are
deduplicated by canonical main-checkout path; each entry's fzf label carries a
`[repo: <basename>]` suffix so same-named worktrees across repos stay
distinguishable. Optional `[repo-keywords]` narrow the scan to repositories
matching the frecency query (best match wins, mirroring `branch`). Wired to
the shell as `zz -W` / `zz --worktree-all`. Also a zjump-only extension (no
zoxide analog). Like the per-repo lookups, it seeds every discovered worktree
path into the frecency database on first use.

### `zjump list [keywords]...`

List the tracked directories ("DIRECTORIES" section by default) plus optional
sections for aliases, branches, and worktrees. This is a zjump-only extension
with **no zoxide equivalent** — zoxide's closest behavior is `zoxide query
--list` (a flag, not a subcommand), and the git sections have no analog at all.

| Flag | Description |
| --- | --- |
| `[keywords]...` | Substrings to filter the DIRECTORIES section (same [matching rules](#matching) as `query`). When `--branches`/`--worktrees` are set, they double as repo-keywords (best DB match wins, mirroring `branch`). |
| `-s`, `--score` | Add a SCORE column to DIRECTORIES (`%6.1f`, clamped to `9999.0`) — same formatting as `query --score`. |
| `-a`, `--all` | Include directories that no longer exist (disables the existence filter) — zoxide's `query --all` semantics. |
| `--aliases` | Add an ALIASES section (sorted by name). |
| `--branches` | Add a BRANCHES section. Repo defaults to `git.RepoRoot($PWD)`; keywords override. Detached worktrees are excluded. |
| `--worktrees` | Add a WORKTREES section. Same repo resolution as `--branches`. Includes detached worktrees (labelled `(detached)`). |
| `--no-dirs` | Suppress the DIRECTORIES section. Combine with `--aliases`/`--branches`/`--worktrees` to print only those. |
| `--all-repos` | Scan every DB entry containing a `.git` and enumerate worktrees across all distinct repos. Adds a REPO leading column to the BRANCHES/WORKTREES sections; deduplicates by canonical main-checkout path. |
| `--json` | Emit a structured JSON object instead of the pretty text table. `directories` is always present (may be `[]`); the optional sections appear **only when requested**, as `[]` (not `null`) even when empty. |

- Bare `zjump list` ≈ `zjump query --list`: a single DIRECTORIES section,
  best-first, with a PATH column. ALIASES/BRANCHES/WORKTREES are absent by
  default; pass `--aliases`, `--branches`, `--worktrees` to opt them in.
- Empty requested sections print their header + a single `(none)` row so you
  can distinguish "asked for, none configured" from a suppressed section.
- If CWD isn't a Git repository and no `[keywords]` are given, BRANCHES /
  WORKTREES print their header + `(none)` without erroring — `[keywords]`
  force repo resolution via the frecency database and error on no match (same
  behavior as `zjump branch`/`zjump worktree` with keywords).
- `list` follows deviation **D-4**: the database file is rewritten after the
  listing **only when** lazy deletions during `db.Stream.Next()` actually
  dirtied it. A pure listing (no stale or excluded entries purged) performs
  no file rewrite.

## Configuration

### `init` flags

When calling `zjump init`, the following flags are available:

- **`--cmd <cmd>`**
  - Changes the prefix of the `zz` and `zzi` commands. Default: `zz`.
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
  - Prevents zjump from defining the `zz` and `zzi` commands. The underlying
    functions remain available as `__zjump_z` and `__zjump_zi` if you want to
    wire them up yourself.
- **`--debug[=PATH]`** (zjump-only addition beyond parity)
  - Bake debug logging into the generated integration script. Every `zz`
    and `zjump` subprocess invocation will write timestamped log lines
    (including errors) to the given file.
  - If `PATH` is omitted, logs go to a default location
    (typically `/tmp/zjump-debug.log`).
  - Resolved to an absolute path at init time.

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
  - When set to `1`, `zz` prints the matched directory before navigating to it.
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
- **`_ZJUMP_PICK_TOP`**
  - The number of frecency-ranked git-worktree directories listed in the
    database-fallback when `-b` / `-w` are called with no argument and the
    current directory is not inside a git repository. Must be a positive
    integer. Default: `10`.
- **`_ZJUMP_AUTO_INDEX_DIRECTORY`**
  - When set to `1`, every `zjump add` (i.e. every `cd` tracked by the shell
    hook) also **seeds** the worktrees of the repository containing the added
    path into the frecency database — once each, with rank 1.0, only when not
    already present — so all of a repo's worktree/branch directories become
    jumpable by `zz <keyword>` without ever having visited them.
  - Deliberately off by default: it adds one `git worktree list` call per `cd`
    into a git repository. Seeded entries are never rank-inflated on repeated
    visits (real `cd`s into a path are the only force that grows its rank).
  - zjump-only extension (no zoxide analog).
- **`_ZJUMP_DOCTOR`**
  - When set to `0`, the shell script's doctor check is disabled. The doctor
    warns once if the tracking hook is missing from `PROMPT_COMMAND` /
    `precmd_functions` after `eval "$(zjump init ...)"`. Set by the generated
    script itself after the first warning; pre-set it to `0` to silence the
    check entirely. Default: `1` (warn once).

## How it works

zjump is a stateless, short-lived binary. Every `zz`/`add`/`query` invocation
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
  with it.
- **D-2** — the `_ZJUMP_*` environment-variable prefix (vs. zoxide's `_ZO_*`).
- **D-3** — `edit` re-sorts after every change, fixing zoxide's session-scoped
  sort-staleness quirk.
- **D-4** — `query` rewrites the database **only when it actually changed** (a
  lazy deletion), rather than on every invocation.
- **D-5** — no Windows `cygpath` handling; zjump is Unix-only.

Also out of scope: the `import` subcommand, shells other than bash/zsh, and
edit-distance matching.

## Development

```sh
go build ./...                    # build everything
go test ./...                     # fast, dependency-free test suite
go test -tags shelltests ./...    # also run real bash/zsh/fzf integration tests
```

For the full development guide — environment setup, testing, linting, code
conventions, debugging, and release workflow — see
[`docs/development.md`](./docs/development.md). For a deep dive into zjump's
internal architecture and package layout, see
[`docs/architecture.md`](./docs/architecture.md).

[`REQUIREMENTS.md`](./REQUIREMENTS.md) defines the scope contract (stable `R-*`
IDs); [`PLAN.md`](./PLAN.md) tracks the roadmap;
[`ARCHITECTURE.md`](./ARCHITECTURE.md) / [`DESIGN.md`](./DESIGN.md) document the
upstream zoxide reference material zjump is built against.

## Contributing

Contributions are welcome! See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for setup,
conventions, scope discipline, and the pull-request process. Please open an
issue first for anything outside the current scope.

## License

zjump is licensed under the [MIT License](./LICENSE).

## Credit

zjump is a from-scratch Go reimplementation inspired by, and designed to be
behaviorally compatible with, [zoxide](https://github.com/ajeetdsouza/zoxide) by
Ajeet D'Souza. All credit for the original design and algorithm goes to that
project; zjump is not affiliated with it.
