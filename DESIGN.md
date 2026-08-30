# Design

## Reference material for reimplementing zoxide's CLI in Go as `zjump`

This document is a ground-truth technical reference for zoxide (https://github.com/ajeetdsouza/zoxide), a Rust CLI tool, compiled from verified line-by-line audits of its source tree (`src/cmd/*`, `src/db/*`, `src/config.rs`, `src/util.rs`, `src/shell.rs`, `templates/*.txt`, `README.md`, `CHANGELOG.md`, `Cargo.toml`) and its build/test/CI configuration (`build.rs`, `.github/workflows/*.yml`, `justfile`, `shell.nix`, packaging metadata). It exists so the team building `zjump` (a Go reimplementation) has an accurate, citable description of what zoxide actually does — not just what its `--help` text or README summarize. Every claim below reflects source-verified behavior of the upstream Rust project; nothing here describes `zjump` itself.

---

## 1. Overview

zoxide's interaction model has three moving parts:

1. **A background tracker.** Every shell (`bash`, `zsh`, `fish`, `posix`/`ksh`/`dash`, `powershell`, `tcsh`, `xonsh`, `elvish`, `nushell`) is configured, via `zoxide init <shell>`, to install a **hook** that silently runs `zoxide add` on the current directory as you navigate. The exact value passed is *not* simply the shell's `$PWD` variable: each generated script defines a `__zoxide_pwd` helper (`\builtin pwd -L`, or `-P` if `_ZO_RESOLVE_SYMLINKS=1`, additionally piped through `cygpath -w` on Windows builds), and the hook invokes `zoxide add -- "$(__zoxide_pwd)"`. Bash's default `pwd` hook mode is a further wrinkle: bash has no native "directory changed" event, so that mode is actually implemented as a `prompt`-style hook (running on every prompt draw) that manually diffs the freshly-computed directory against a *cached* `${__zoxide_oldpwd}` variable, only invoking `zoxide add` with that cached value when it differs from the current one — see §4 for the full per-shell breakdown of hook mechanics. Each invocation increments a per-path **rank** (a raw frecency counter) and updates that path's `last_accessed` timestamp in an on-disk database (`db.zo`, binary-encoded via `bincode`, located under the OS's local data directory unless overridden).
2. **A frecency-ranked index.** Every stored directory has a `rank` (frequency-like counter, incremented by `zoxide add`) and a `last_accessed` epoch. When you query, zoxide doesn't sort by raw rank — it computes a **decayed score** per entry: `rank` multiplied by 4x if accessed within the last hour, 2x within the last day, 0.5x within the last week, or 0.25x if older. This is the classic "frecency" (frequency + recency) heuristic: a directory you visited constantly last year ranks below one you've touched twice today.
3. **A jump command.** `zoxide init` also generates shell functions — by default named `z` and `zi` (the prefix is configurable via `--cmd`) — that replace routine `cd` typing. `z <keywords>` resolves a substring-matched, highest-scoring directory and `cd`s into it in one step; `zi <keywords>` does the same but funnels ambiguous matches through an interactive `fzf` picker instead of auto-selecting the top hit. The net effect: instead of typing `cd ../../projects/foo/backend`, a user who has visited that directory before can type `z backend` (or similar) and land there directly.

zoxide is explicitly framed (verbatim, `README.md`) as: *"a smarter cd command, inspired by z and autojump. It remembers which directories you use most frequently, so you can 'jump' to them in just a few keystrokes. zoxide works on all major shells."* It positions itself as a strict, backward-compatible superset of `cd` (see §9) rather than a wholly separate mental model — `z ~/foo`, `z foo/`, `z ..`, and `z -` all work exactly as the equivalent `cd` invocation would, in addition to the fuzzy-keyword jump behavior.

The CLI itself (the `zoxide` binary) is a thin, stateless-per-invocation tool: it has no daemon and no persistent process. Every `zoxide add`/`zoxide query`/etc. invocation opens the database file, does one unit of work, and closes it (with an atomic temp-file-then-rename write on any mutation). All "background tracking" is really just the shell hook synchronously shelling out to `zoxide add` on every prompt/cd — there is no separate watcher process.

---

## 2. Command Reference

zoxide's CLI surface is exactly six top-level subcommands, defined via `clap`'s derive macros in a single source file (`src/cmd/cmd.rs`): **`add`**, **`edit`**, **`import`**, **`init`**, **`query`**, **`remove`**. There is no `zoxide help` subcommand (`disable_help_subcommand = true`); help is only available via `-h`/`--help`, and `--version` is propagated to every subcommand. Every subcommand's generated `--help` output appends a fixed "Environment variables" block listing all six `_ZO_*` variables (see §3).

### 2.1 `zoxide add`

**Purpose:** Add a new directory to the database, or increment the rank of an existing entry. This is the command the shell hook invokes on every tracked navigation; it is rarely typed by hand.

**Usage:** `zoxide add [OPTIONS] <PATHS>...`

| Flag | Short | Type | Default | Effect |
|---|---|---|---|---|
| `<PATHS>...` | — | positional, one or more | required | Directories to add/increment. `num_args = 1..`. |
| `--score` | `-s` | `f64` | none (falls back to `1.0`) | "The rank to increment the entry if it exists or initialize it with if it doesn't." |

**Step-by-step behavior:**
1. Loads `_ZO_EXCLUDE_DIRS` and `_ZO_MAXAGE` configuration up front — before opening the database — so a malformed env var errors out even if all paths would otherwise be skipped.
2. Reads the current time (errors with "system clock set to invalid time" if the clock predates the Unix epoch).
3. Opens the database (creating the data directory if missing; see §3, `_ZO_DATA_DIR`).
4. For each path argument:
   - Resolves it: full filesystem canonicalization (symlinks followed) if `_ZO_RESOLVE_SYMLINKS=1`, otherwise a purely lexical resolution (no filesystem access, symlinks left as-is).
   - Converts to a UTF-8 string; errors "invalid unicode in path: {path}" on non-UTF-8 paths.
   - **Silently skips** (no error) the path if it contains `\n`/`\r`, or if it matches any `_ZO_EXCLUDE_DIRS` glob pattern.
   - **Errors** `"not a directory: {path}"` if the resolved path is not, in fact, a directory.
   - Computes the increment `by = --score value, or 1.0 if omitted`.
   - Updates the database entry: if a `Dir` with this exact path string already exists, `rank = (rank + by).max(0.0)` (floored at zero, never negative) and `last_accessed = now`; otherwise inserts a brand-new entry with `rank = by.max(0.0)`, `last_accessed = now`.
5. If any add actually changed the database, runs the **aging pass** (see below) using the `_ZO_MAXAGE` threshold.
6. Saves the database (atomic write; no-op if nothing changed).

**Aging algorithm** (also used by `import`, see §6): sum every entry's raw `rank`. If that sum exceeds `_ZO_MAXAGE` (default `10000.0`), scale *every* entry's rank by `factor = 0.9 * max_age / total_age`, then delete (order-disturbing O(1) removal) any entry whose *post-scaling* rank falls below `1.0`. This keeps the total rank budget roughly bounded and prunes long-neglected, low-weight entries — it does not delete entries just because they're old in wall-clock time, only because their *decayed rank share* has become negligible.

**Error cases:** malformed `_ZO_EXCLUDE_DIRS`/`_ZO_MAXAGE`, clock error, database-open I/O errors, path-resolution errors, non-UTF-8 paths, `"not a directory: {path}"`, database save I/O errors.

### 2.2 `zoxide query`

**Purpose:** Search the database for a directory matching the given keywords and print the best match (or list/interactively select among matches). This is the command `z`/`zi` shell functions invoke under the hood.

**Usage:** `zoxide query [OPTIONS] [KEYWORDS]...`

| Flag | Short | Type | Default | Effect |
|---|---|---|---|---|
| `[KEYWORDS]...` | — | positional, zero or more | `[]` | Substrings to match against candidate paths. |
| `--all` | `-a` | flag | `false` | Show unavailable (currently-nonexistent) directories too; disables the filesystem-existence filter and its associated lazy deletion of stale entries. |
| `--interactive` | `-i` | flag | `false` | Use interactive `fzf`-based selection instead of auto-picking the top match. Conflicts with `--list`. |
| `--list` | `-l` | flag | `false` | List every matching directory instead of just the best one. Conflicts with `--interactive`. |
| `--score` | `-s` | flag | `false` | Print each result's decayed frecency score alongside its path. |
| `--exclude <path>` | — | `Option<String>` | `None` | Exclude a specific path (typically the current directory) from results, compared as a raw string against stored paths — not resolved or canonicalized. |
| `--base-dir <path>` | — | `Option<String>` | `None` | Only return matches that are (component-wise) under this directory; also compared verbatim, never resolved. |

**Step-by-step behavior:**
1. Opens the database, runs the query, then **unconditionally** saves the database afterward — even if the query found no match or errored — because of how the underlying result-chaining is implemented. This matters operationally: sorting the in-memory entries by score (which every query does) always marks the database "dirty," so **essentially every `zoxide query` call performs a real atomic rewrite of the database file**, not just ones that added/removed/excluded something.
2. Builds a lazily-evaluated match stream over all entries, applying — in this exact order, per candidate — (a) keyword filtering, (b) `--base-dir` filtering, (c) `_ZO_EXCLUDE_DIRS` glob filtering (evaluated *inside* the match stream itself), and (d) (unless `--all`) filesystem-existence filtering. **The CLI's own `--exclude <path>` flag is a separate, independent mechanism and is not part of this in-stream filter pipeline at all** — it's a plain string-equality check performed in the calling code (the default/`--list`/`--interactive` implementations), applied to each candidate *after* it emerges from the stream. This distinction has real behavioral consequences: an `_ZO_EXCLUDE_DIRS` glob match causes the matching entry to be **permanently deleted** from the database the moment it's evaluated (see the lazy-deletion bullet below); a `--exclude` match never deletes anything — it just causes that one candidate to be skipped for the current invocation, while the entry itself remains untouched in the database. A reimplementation must keep these two exclusion mechanisms structurally separate rather than merging them into one filter.
3. Dispatches based on flags: `--interactive` wins first, then `--list`, else the default "first match" mode.

**Keyword matching algorithm** (case-insensitive, substring-based, *not* fuzzy/edit-distance):
- No keywords → every candidate matches.
- The **last** keyword must appear (rightmost occurrence) within the final path component — nothing after that match may contain a path separator.
- Each preceding keyword (processed right-to-left) must then be found as a substring strictly to the left of the previous match, in order — keywords cannot overlap or appear out of order.
- Example: keywords `["foo", "o", "bar"]` do **not** match `/foo/bar`, because after `"bar"` and `"o"` consume overlapping text, no room is left for `"foo"` — even though each keyword individually occurs in the path.

**Scoring/ordering:** entries are sorted ascending by decayed score (`rank × {4, 2, 0.5, 0.25}` per the recency bucket — see §1) and then walked in reverse, so the **highest-scoring match is always returned/shown first**.

**Lazy deletion side effects (every query, not just `--all`-disabled ones):**
- Any candidate matching an `_ZO_EXCLUDE_DIRS` glob is deleted from the database the moment it's encountered inside the match stream — this is the *only* one of the two exclusion mechanisms described above that deletes anything.
- Any candidate that fails the existence check (directory no longer exists) **and** hasn't been accessed in the last ~90 days (`last_accessed` older than `now − 3×30 days`) is also deleted. A missing-but-recently-used directory (e.g. an unmounted drive) is hidden from results but *not* deleted, in case it reappears.
- `--all` disables the existence check entirely, and therefore this second deletion path too.

**Mode-specific behavior:**
- **Default (first match):** prints the single best match. Errors `"no match found"` if nothing matches. If the top match equals `--exclude`, keeps pulling the next-best match; errors `"you are already in the only match"` if none remain.
- **`--list`:** prints every matching path, one per line (with score prefix if `--score`), skipping the `--exclude`d path.
- **`--interactive`:** streams matches into an external `fzf` process as they're found (skipping the excluded path) and returns the user's selection; strips the leading score column unless `--score` was passed. See §5 for exact fzf wiring.

**Error cases:** malformed `_ZO_EXCLUDE_DIRS`, clock error, `"no match found"`, `"you are already in the only match"`, fzf-related errors (§5, §8), I/O errors.

### 2.3 `zoxide remove`

**Purpose:** Remove one or more directories from the database by hand.

**Usage:** `zoxide remove [PATHS]...`

| Flag | Short | Type | Default | Effect |
|---|---|---|---|---|
| `[PATHS]...` | — | positional, zero or more | `[]` | Paths to remove. Command is a no-op if empty. |

**Step-by-step behavior:** for each path, first tries an exact string match against stored paths. If that fails, lexically resolves the path to an absolute form (no symlink resolution) and retries; if the resolved string is identical to the original (retrying would be pointless) or the retry also fails, errors `"path not found in database: {path}"`. Saves the database once all paths are processed. Removal itself is an order-disturbing O(1) operation (the database's last entry is moved into the freed slot) rather than a stable/index-preserving delete — see §2.5 for why this matters when combined with `zoxide edit`'s session-scoped sort ordering.

**Error cases:** database I/O errors, path-resolution errors, non-UTF-8 paths, `"path not found in database: {path}"`.

### 2.4 `zoxide init`

**Purpose:** Print the shell-specific initialization script (functions, hooks, completions) that a user sources from their shell's rc file. This is the sole entry point that wires zoxide into a live shell session — see §4.

**Usage:** `zoxide init [OPTIONS] <SHELL>`

| Flag | Short | Type | Default | Effect |
|---|---|---|---|---|
| `<SHELL>` | — | positional enum, required | — | One of `bash`, `elvish`, `fish`, `nushell`, `posix` (alias `ksh`), `powershell`, `tcsh`, `xonsh`, `zsh`. |
| `--no-cmd` (alias `--no-aliases`) | — | flag | `false` | Suppresses generation of the `z`/`zi` (or custom-prefix) commands entirely — only the tracking hook and completions are emitted. |
| `--cmd <CMD>` | — | `String` | `"z"` | Renames the generated jump command; the interactive variant is always `<cmd>i` (e.g. `--cmd j` → `j`/`ji`). |
| `--hook <HOOK>` | — | enum | `"pwd"` | One of `none`, `prompt`, `pwd` — see §4 for exact semantics per shell. |

**Step-by-step behavior:** resolves the effective `cmd` (`None` if `--no-cmd`, else the `--cmd` value), reads `_ZO_ECHO`/`_ZO_RESOLVE_SYMLINKS` from the environment, renders the matching shell template with these options baked in, and writes the result to stdout (the user is expected to `eval`/`source` this output, typically via `eval "$(zoxide init bash)"` or similar). This write goes through the same broken-pipe-tolerant path described in §8.

**Error cases:** template-rendering failure (essentially never in practice), stdout write errors (broken pipe → silent exit, see §8).

### 2.5 `zoxide edit`

**Purpose:** Interactively browse and mutate the database directly (view scores, delete stray entries, bump/demote specific directories) via an `fzf`-driven UI. Also exposes four **hidden** subcommands used internally by that UI's key bindings — not meant to be typed by users directly, but part of the real, documented-in-source CLI surface.

**Usage (interactive mode):** `zoxide edit`
**Usage (internal, hidden subcommands):** `zoxide edit <decrement|delete|increment> <PATH>` / `zoxide edit reload`

| Subcommand | Args | Effect |
|---|---|---|
| `decrement` | `path` (positional) | `rank = (rank − 1.0).max(0.0)` on the matching entry (no floor-triggered deletion; a repeatedly decremented entry sits at rank 0 until manually deleted or swept by a later aging pass). |
| `increment` | `path` (positional) | `rank = rank + 1.0` on the matching entry. |
| `delete` | `path` (positional) | Removes the entry outright (order-disturbing O(1) removal — see the ordering caveat below). |
| `reload` | none | Pure no-op mutation; used purely to re-dump the current entry list. |

**Caveat on `increment`/`decrement` and `last_accessed`:** these only leave `last_accessed` untouched when the target path is **already present** in the database — the normal case, since the path comes from a row `fzf` is currently displaying. If the path is *not* found (reachable if the database changed underneath the UI between a reload and a key-press), `increment`/`decrement` fall back to inserting a brand-new entry with `last_accessed = now`, exactly like a fresh `zoxide add` would.

**Step-by-step behavior:**
- **Subcommand invocations** (fired by `fzf` key-binding `reload(...)` actions, not typically run manually): open the DB, apply the one mutation described above if any, save, then print **every** entry (highest score first, as of the ordering established below) as tab-separated `score\tpath` NUL-terminated records to stdout — this becomes `fzf`'s next candidate list.
- **No subcommand (interactive mode):** sorts the entire database ascending by decayed score (this always marks the DB dirty, even if the order didn't actually change) and saves it, then spawns `fzf` and waits for it to exit. The *initial* population of the `fzf` list happens via a `start:` key binding that immediately triggers `zoxide edit reload`. The return value of the `fzf` session (i.e., any accepted line) is discarded — only errors propagate; `edit` never `cd`s or prints a selected path.

**Ordering caveat (session-scoped sort staleness):** the ascending-by-score sort that establishes "highest score first" ordering runs **once**, at the very start of the interactive session, before `fzf` is even spawned — it is *not* re-run after every mutation. `delete` uses the same order-disturbing `swap_remove` semantics as `zoxide remove` (§2.3): removing an entry moves the database's *last* element into the freed slot. Because each `ctrl-d`/`ctrl-w`/`ctrl-s`/`ctrl-r` reload is a **fresh subprocess** that simply reopens the database from disk, mutates, and re-saves (without re-sorting), a single `ctrl-d` delete can silently relocate whatever entry used to occupy the last slot (which, immediately after the initial sort, was the *highest-scoring* entry) into an arbitrary earlier position. `increment`/`decrement` don't reorder anything (they mutate the matching entry in place), but repeated deletes within one edit session can leave the displayed "highest score first" list increasingly stale relative to entries' true current score, until the session ends and a fresh `zoxide edit` invocation re-triggers the sort. A faithful reimplementation should either accept this quirk for parity or deliberately decide to re-sort after every mutating reload.

**Key bindings and exact `fzf` invocation:** see §5 (Interactive Mode UX) for the full, verified binding table. `edit` does **not** consult `_ZO_FZF_OPTS` at all — its `fzf` argument list is fixed regardless of that variable.

**Error cases:** clock error, database I/O errors, `fzf`-not-found (`"could not find fzf, is it installed?"`), the `fzf` exit-code error table in §5/§8 (including a silent exit on Ctrl-C).

### 2.6 `zoxide import`

**Purpose:** Bulk-import directory history from another frecency/history tool into zoxide's database.

**Usage:** `zoxide import [OPTIONS] <FROM>`

| Flag | Short | Type | Default | Effect |
|---|---|---|---|---|
| `<FROM>` | — | positional subcommand, required | — | One of `atuin`, `autojump`, `fasd`, `z`, `z.lua`, `zsh-z`. |
| `--merge` | — (no short) | flag, `global = true` | `false` | Required to import into a non-empty database; see §6. |

**Step-by-step behavior:** opens the DB; if it's non-empty and `--merge` wasn't passed, bails immediately with `"current database is not empty, specify --merge to continue anyway"` — no partial import occurs. Otherwise dispatches to the source-specific parser (see §6 for each backend's file location/format) via a shared driver that: loads `_ZO_EXCLUDE_DIRS` once; for each successfully-parsed record, skips it if it matches an exclude glob, otherwise inserts it unconditionally (duplicates are expected and tolerated at this stage — no existing-entry check, unlike `add`); for each malformed record, logs `"{source}:{line}: {reason}"` to stderr and continues (a single bad row never aborts the import). After the full source is consumed, if anything was actually imported: deduplicates the database (merging same-path entries by **summing** their ranks and taking the **max** of their `last_accessed` values) and runs the same aging pass `add` uses, using `_ZO_MAXAGE`. Finally saves.

**Error cases:** DB non-empty without `--merge`, malformed `_ZO_EXCLUDE_DIRS`/`_ZO_MAXAGE`, source-fetch failure (e.g. missing file, `atuin` binary not found/not on PATH — per-record parse errors are non-fatal and just logged), save I/O errors.

---

## 3. Environment Variables

All six are read via `config.rs` and all six appear in the auto-generated help-text footer of every subcommand.

| Variable | Purpose | Default |
|---|---|---|
| `_ZO_DATA_DIR` | Directory holding zoxide's data file (`db.zo`). | OS local-data directory + `"zoxide"` (e.g. `~/.local/share/zoxide` on Linux via XDG) if unset. **The resolved directory must be an absolute path, and this is validated unconditionally** — the same `ensure!(dir.is_absolute(), ...)` check runs regardless of whether the value came from `_ZO_DATA_DIR` or from the default `dirs::data_local_dir()`-based path. In practice the default is always already absolute, so this check only ever surfaces as a user-facing error when `_ZO_DATA_DIR` itself is explicitly set to a relative path. |
| `_ZO_ECHO` | If set, print the matched directory before navigating into it (consumed by the shell templates, not by any single `cmd/*` file). | `false` — only `true` when the variable's value is exactly the string `"1"`. |
| `_ZO_EXCLUDE_DIRS` | List of directory glob patterns to exclude from `add`, `import`, and lazy-purged during `query`. | If unset: a single pattern matching the user's home directory **exactly** (glob-escaped, so it does *not* match subdirectories of home). If set: an OS path-list (`:` on Unix, `;` on Windows) of glob patterns; invalid UTF-8 or invalid glob syntax errors out. |
| `_ZO_FZF_OPTS` | Custom flags passed to the `fzf` subprocess. **Only consulted by `zoxide query --interactive`** — never by `zoxide edit`. | Unset — zoxide's own built-in `fzf` args + preview pane are used (see §5). If set, it fully replaces the built-in args (via `FZF_DEFAULT_OPTS`) **and disables the preview pane** as a side effect. |
| `_ZO_MAXAGE` | Maximum total *raw rank* sum across all entries before the aging/pruning pass kicks in. | `10000.0` (parsed as an unsigned 32-bit integer, then treated as float). |
| `_ZO_RESOLVE_SYMLINKS` | If set, resolve symlinks when storing paths (`add`) and when checking existence (`query`). | `false` — only `true` when the variable's value is exactly `"1"`. |

Two additional, tool-specific (not `_ZO_*`) environment variables matter for `import`: `_Z_DATA` (z/zsh-z data file location), `_FASD_DATA` (fasd), `_ZL_DATA` (z.lua), `ZSHZ_DATA` (zsh-z, preferred over `_Z_DATA`), plus the standard `XDG_DATA_HOME`/`APPDATA`/`HOME` used by various backends' default-path detection.

---

## 4. Shell UX

### Generated commands

`zoxide init <shell> [--cmd X] [--no-cmd]` emits, by default, two shell functions: **`z`** (jump, fuzzy/exact match with automatic top-pick) and **`zi`** (jump with interactive `fzf` disambiguation). The prefix is fully configurable (`--cmd myj` → `myj`/`myji`); `--no-cmd`/`--no-aliases` suppresses both, leaving only the tracking hook and completions active — useful if a user wants to wire the underlying `zoxide query`/`zoxide add` calls into their own custom keybindings instead.

`z`'s own argument-dispatch logic (before ever calling `zoxide query`):
- No args → `cd` to home.
- `-` alone → `cd` to `$OLDPWD` (previous directory).
- A single argument that's already an existing directory → `cd` directly into it (no query performed).
- `-- <path>` (explicit passthrough) → `cd` directly (bash/zsh/fish only; POSIX has no such branch).
- Otherwise → shells out to `zoxide query --exclude "$PWD" -- "$@"` and `cd`s to the result.

`zi` always runs `zoxide query --interactive -- "$@"`, regardless of argument count.

### The three hook modes and their trade-offs

| Mode | When it fires | Trade-off |
|---|---|---|
| `none` | Never — no tracking at all. | No overhead, but the database never grows from normal `cd` usage; entries only appear via explicit `zoxide add`/`import`. |
| `prompt` | On every prompt draw (i.e., after every completed command), regardless of whether the directory actually changed. | Simple and universally implementable (even on POSIX sh, via a `$PS1` command-substitution hack), but adds overhead on every single command and can over-count directories you merely `ls`'d through in a subshell. This is the **only** mode POSIX/`dash`/`ksh` support — `pwd` mode is explicitly unimplemented there and prints a diagnostic recommending `prompt` instead. |
| `pwd` (default) | Only when the working directory changes. | Precise and lower-overhead where a native "directory changed" event exists (zsh's `chpwd_functions`, fish's `--on-variable PWD`). **In bash, there is no such native event** — bash's `pwd` mode is actually implemented as a `prompt`-mode hook that manually diffs the current directory (via `__zoxide_pwd`) against a cached `$__zoxide_oldpwd` variable on every prompt draw, only calling `zoxide add` (with that cached value) when they differ. So bash's "pwd" mode still runs on every prompt, it just conditionally skips the `add` call. |

### Per-shell differences worth knowing

- **bash:** requires bash ≥4.4 for Space-Tab interactive completion (uses `${var@Q}` quote-expansion). Installs its hook into `PROMPT_COMMAND`, detecting whether that variable is already an array or a string and appending accordingly. `pwd` mode is emulated via manual diffing (see above), not a real event.
- **zsh:** has native `precmd_functions` (prompt mode) and `chpwd_functions` (pwd mode) arrays — both are true events, unlike bash. Tab completion integrates via `compdef` plus a ZLE (Zsh Line Editor) widget, so no terminal-escape-sequence trickery is needed for prompt redraw — it uses `zle reset-prompt`/`zle accept-line` directly.
- **fish:** has native `--on-event fish_prompt` and `--on-variable PWD` hook mechanisms — the cleanest of the three. `cd` is wrapped by first renaming fish's own builtin `cd` function to a private name, so `z`/`zi` can safely alias over `cd`-like behavior without infinite recursion. Tab completion uses fish's `commandline --function repaint execute` to directly mutate/execute the command line in place — no escape-sequence hack required (unlike bash/zsh).
- **posix (dash/ksh/generic sh):** `pwd` hook mode is **not supported** — selecting it prints (to stdout, not stderr — a documented quirk) a message recommending `prompt` mode instead. `prompt` mode is implemented by injecting a command substitution directly into `$PS1`, which only works on shells that expand command substitutions when drawing the prompt. No tab-completion support at all (POSIX sh has no completion framework).
- **All shells' interactive selection (`zi`, and `z` Space-Tab completion) shell out to the external `fzf` binary** — it is not bundled or built-in, and it is not `skim`. If `fzf` isn't installed, the relevant command fails with `"could not find fzf, is it installed?"`. The minimum supported `fzf` version is **v0.51.0** (this is the version the upstream project itself enforces/tests against; older releases are not guaranteed to support the flags zoxide passes — see §5). Space-Tab completion is explicitly bash 4.4+/fish/zsh-only (not available on POSIX shells, and not supported at all inside Warp, which provides its own completions).

---

## 5. Interactive Mode UX

zoxide requires **`fzf` v0.51.0 or newer** for both `zoxide query --interactive` and `zoxide edit`; this is the minimum version the upstream project documents and tests against, and older `fzf` releases are not guaranteed to support all of the flags described below.

### `zoxide query --interactive`

Spawns the external `fzf` binary as a child process, feeding it one NUL-terminated, tab-delimited record per candidate directory — `"{score:>6.1}\t{path}\0"` — as they're streamed from the ranked match iterator (best-first). `fzf` is configured with `--delimiter=\t --nth=2 --read0` so it searches/sorts on the path field only, ignoring the leading score column.

**Default arguments** (used unless `_ZO_FZF_OPTS` is set):
```
--exact
--no-sort
--bind=ctrl-z:ignore,btab:up,tab:down
--cycle
--keep-right
--border=sharp
--height=45%
--info=inline
--layout=reverse
--tabstop=1
--exit-0
```
plus a preview pane (Unix only — no-op on Windows) running `ls -Cp` (with `--color=always --group-directories-first` on Linux specifically) against the highlighted path, in a `down,30%,sharp` window.

**`_ZO_FZF_OPTS` override:** if set, its raw value is passed via the `FZF_DEFAULT_OPTS` environment variable instead of the built-in argument list above, and — as a side effect — the preview pane is **not** enabled in this branch.

**Selection behavior:** Enter accepts the highlighted line normally (default `fzf` behavior, no custom binding). On acceptance, zoxide strips the leading 7 characters (6-char score field + 1 separator) unless `--score` was also passed to `zoxide query`, in which case the full scored line is printed.

### `zoxide edit`

Spawns `fzf` populated entirely through a `start:reload(zoxide edit reload)` binding (not via direct stdin streaming, unlike `query -i`) — every visible list refresh is a fresh subprocess invocation of one of the hidden `zoxide edit <subcommand>` commands. As detailed in §2.5, this per-subprocess reload architecture means the ascending-by-score sort is only ever established once, at session start; a `ctrl-d` delete's `swap_remove` semantics can leave the "highest score first" ordering stale for the rest of that session.

**Complete key-binding table:**

| Key | Action |
|---|---|
| `start` (on launch) | `reload(zoxide edit reload)` — populates the initial list, highest score first. |
| `ctrl-r` | `reload(zoxide edit reload)` — manual refresh, no mutation. |
| `ctrl-d` | `reload(zoxide edit delete {2..})` — deletes the highlighted entry (path = field 2 onward), then reloads. |
| `ctrl-w` | `reload(zoxide edit increment {2..})` — bumps the highlighted entry's rank by `+1.0`, then reloads. |
| `ctrl-s` | `reload(zoxide edit decrement {2..})` — drops the highlighted entry's rank by `-1.0` (floored at `0.0`), then reloads. |
| `tab` | `down` — move selection down. |
| `btab` (shift-tab) | `up` — move selection up. |
| `enter` | `abort` — **does not** select/cd anywhere; simply ends the edit session (exits via the same code path as Ctrl-C, silently). |
| `ctrl-z` | `ignore` — explicitly neutralized (prevents an accidental default binding). |
| `double-click` | `ignore` — explicitly neutralized (fzf's default double-click action is normally `accept`, which would otherwise close the session). |

**Other fixed options:** `--exact --no-sort --cycle --keep-right`, `--border=sharp` with label `"  zoxide-edit  "`, a three-line `--header` showing the reload/delete/increment/decrement legend plus a `SCORE`/`PATH` column header, `--info=inline --layout=reverse --padding=1,0,0,0 --color=label:bold --tabstop=1`, and the preview pane **always enabled** (unconditionally — unlike `query -i`, this doesn't depend on `_ZO_FZF_OPTS`, which `edit` never reads).

### Exit-code semantics (shared by both interactive modes)

| `fzf` exit code | zoxide's response |
|---|---|
| `0` | Success — returns the selected line(s). |
| `1` | `"no match found"` error. |
| `2` | `"fzf returned an error"` error. |
| `130` | **Silent exit**, code 130 — the user pressed Ctrl-C or Esc (or, in `edit`, pressed Enter, which is bound to `abort`). No error text is printed; the process just exits with status 130. |
| `128`–`254`, or the process was signaled | `"fzf was terminated"` error. |
| anything else | `"fzf returned an unknown error"` error. |

These fzf-specific codes are one instance of a broader, shared silent-exit/error-reporting convention that also covers ordinary broken-pipe conditions on any stdout write, in any subcommand — see §8 for the full picture.

---

## 6. Import UX

### Supported source tools

| Source | Data location | Record format | Per-record transform |
|---|---|---|---|
| **atuin** | Not a file — spawns `atuin history list --format={time}\t{directory} --print0` as a subprocess. Fails if `atuin` isn't installed/on `PATH`. | NUL-separated records of `{time}\t{directory}`; `{time}` is `YYYY-MM-DD HH:MM:SS` UTC. | `rank` hardcoded to `1.0` per surviving record; `last_accessed` = parsed timestamp. Consecutive duplicate directories (repeated visits with no directory change in between) are collapsed to one record. |
| **autojump** | OS-dependent: `~/Library/autojump/autojump.txt` (macOS), `%APPDATA%/autojump/autojump.txt` (Windows), else `$XDG_DATA_HOME/autojump/autojump.txt` or `~/.local/share/autojump/autojump.txt`. | One entry per line, tab-separated `rank\tpath`. | `rank` is passed through a **sigmoid** function, `1 / (1 + e^-x)`, to normalize autojump's very differently-scaled ranking algorithm into zoxide's range; `last_accessed` is hardcoded to `0` (autojump's file has no per-entry timestamp), which places every imported entry in the oldest/lowest decay bucket until next visited. |
| **fasd** | `$_FASD_DATA` or `~/.fasd`. | One entry per line, `path|rank|last_accessed` (pipe-delimited, split from the right so paths containing `|` are preserved). | None — values imported verbatim. |
| **z** | `$_Z_DATA` or `~/.z`. | Same `path|rank|last_accessed` format. | None. |
| **z.lua** | `$_ZL_DATA` (unless it looks like a Windows-style path on a non-Windows OS) or `~/.zlua`; falls back to the Fish-style path (`$XDG_DATA_HOME/zlua/zlua.txt` or `~/.local/share/zlua/zlua.txt`) if the primary file is missing. | Same `path|rank|last_accessed` format. | None. |
| **zsh-z** | `$ZSHZ_DATA`, else legacy `$_Z_DATA`, else `~/.z`. | Same `path|rank|last_accessed` format. | None. |

### `--merge` semantics

`--merge` is a global flag on `zoxide import`. Without it, **importing into any non-empty database is refused outright** — the command bails immediately with `"current database is not empty, specify --merge to continue anyway"` before touching anything. This is a deliberate safety rail against accidentally clobbering/duplicating an existing, populated zoxide database. `--merge` simply lifts that guard; it does not change the parsing or insertion logic in any other way — the same shared import pipeline runs regardless.

### Conflict/duplicate handling

Import does **not** use the same "find-existing-entry-and-increment" logic that `zoxide add` uses. Instead:
1. Every successfully-parsed, non-excluded record is inserted **unconditionally** as a brand-new database entry — duplicates (e.g. the same path appearing multiple times across the source file, or already present from a prior import) are expected and tolerated at insertion time.
2. **After** the entire source has been consumed, if anything was actually inserted, a **deduplication pass** runs: entries are sorted by path, and any adjacent entries sharing the exact same path string are merged — the merged entry's `rank` is the **sum** of the duplicates' ranks, and its `last_accessed` is the **max** (most recent) of the duplicates' timestamps.
3. Finally, the same aging/pruning pass used by `zoxide add` runs against `_ZO_MAXAGE`, so a very large import can immediately trigger proportional rank-scaling and removal of newly-inserted low-rank entries.

Malformed individual records (unparseable lines) do not abort the import — each bad record is logged to stderr as `"{source}:{line}: {reason}"` and skipped, with the rest of the import proceeding normally. Records whose path matches an `_ZO_EXCLUDE_DIRS` glob are silently skipped (not counted as errors, not inserted, not logged).

---

## 7. On-Disk Database Format

This section documents `db.zo`'s exact binary layout — necessary ground truth for `zjump` if it needs to read or write a compatible file, or to design its own equivalent persistence format.

**File identity and location:** the file is always named exactly `db.zo`, joined onto `_ZO_DATA_DIR` (see §3). On open, the path is best-effort canonicalized (falling back to the uncanonicalized path if that fails — expected on a first run, since canonicalization requires the target to already exist). If the file doesn't exist, an empty in-memory database is used and the file itself is not created until the first save with actual changes; the containing data directory *is* created eagerly, though.

**Header:** a `u32` format version, currently `3`, written first, little-endian. On load, a version mismatch is a hard error (`"unsupported version (got {v}, supports 3)"`); a file shorter than 4 bytes (too short to even hold the version field) errors `"could not deserialize database: corrupted data"`.

**Encoding:** the payload (a length-prefixed list of directory entries) is encoded via `bincode` (crate major version 1), using **fixed-width ("fixint") little-endian encoding — not variable-length ("varint") encoding, and not native/host-endianness.** The on-disk format is therefore little-endian on every supported platform, independent of host CPU byte order. This is a deliberate, explicit choice on the read side (and is bincode's default behavior for its plain top-level serialize/deserialize functions on the write side), and it's what makes the write and read paths byte-compatible with each other.

**Deserialization size guard:** reads are bounded to **32 MiB** (`32 << 20` bytes) specifically to fail fast and predictably on a corrupted or truncated file, rather than surfacing an obscure/unbounded-allocation error from the decoder.

**Per-entry byte layout**, once past the 4-byte version header:

| Field | Size | Encoding |
|---|---|---|
| entry count (list length) | 8 bytes | `u64`, little-endian |
| path length (per entry) | 8 bytes | `u64`, little-endian |
| path bytes (per entry) | variable (= path length) | raw UTF-8, no terminator |
| rank (per entry) | 8 bytes | `f64`, IEEE-754 little-endian |
| last_accessed (per entry) | 8 bytes | `u64`, little-endian (Unix epoch seconds) |

Each entry therefore costs 24 bytes of fixed overhead (8-byte length prefix + 8-byte rank + 8-byte timestamp) plus the raw byte length of its path string. Struct fields carry no name/type tags — just raw values, serialized in declaration order, with no framing between fields.

**Zero-copy read (implementation detail, not format-relevant for a Go port):** on load, the whole file is read into memory as one buffer (a single full-file read — not memory-mapped; there is no `mmap` dependency anywhere in the project), and each entry's path is deserialized as a slice borrowed directly from that buffer rather than copied into a fresh heap string, for performance. This has no bearing on the on-disk byte layout itself.

**Atomic writes:** every save writes a full new copy of the file to a randomly-named temporary file in the *same* directory as the target, flushes/syncs it to durable storage, best-effort re-applies the original file's owning uid/gid on Unix, then renames it over the target (a single atomic `rename` on Unix; retried a few times on `PermissionDenied` on Windows, since rename isn't guaranteed atomic there). On any failure mid-sequence the temp file is cleaned up and the original file is left untouched — a crash or kill mid-write can never leave `db.zo` truncated or corrupted.

---

## 8. Error Handling & Exit-Code Conventions

zoxide funnels every "this isn't really an error, just stop quietly" condition through one shared mechanism (a `SilentExit { code }` internal error type) rather than printing anything and setting a nonzero exit code the normal way. Two distinct situations trigger it:

1. **Broken pipe on *any* stdout (or fzf-stdin) write, in every subcommand that writes output — not just the interactive-fzf commands.** Concretely: `zoxide query` (default/`--list` modes), `zoxide edit`'s per-subcommand database dump (the `reload`/`increment`/`decrement`/`delete` list printed back to `fzf`), and `zoxide init`'s script output are all wrapped so that a broken-pipe I/O error (e.g. piping `zoxide query --list` into `head`, or the reading process exiting early) is caught and converted into a silent, successful-looking exit with code `0`, rather than surfacing as a printed error. Any *other* kind of I/O error on the same write is still reported normally, with a `"could not write to {device}"`-style context message. This is a normal, expected condition for any Unix-pipeline tool and needs to be replicated deliberately — it is not a fzf-specific special case.
2. **User cancels an interactive `fzf` session** (Ctrl-C, Esc, or — in `zoxide edit`'s case — pressing Enter, which is deliberately rebound to the same `abort` action) — `fzf` exits with process status `130`, and zoxide's own process then exits silently with the same code `130`, matching the conventional `128+SIGINT` exit-code scheme, again with no printed error text. This is documented per-command in §5's exit-code table but is really the same underlying mechanism as case 1, just triggered by a different upstream signal.

For genuine (non-silent) errors, zoxide prints `zoxide: {error:?}` to stderr — using the **Debug** formatting of its internal error type (which wraps a general-purpose chained-error type), not a plain single-line `Display` format. This means the printed message is the **entire causal chain** of attached context (every "could not ..." layer added as the error propagated up through the call stack), not just the innermost/root message — so real errors tend to appear as multi-line output rather than one terse line. Additionally, zoxide unconditionally strips backtrace-related environment variables from its own process environment at startup, so backtraces are never appended to error output regardless of what the invoking shell has set.

**Takeaway for `zjump`:** a faithful Go port needs (a) a broken-pipe-tolerant, silent-exit-0 path on *every* stdout write site, not just the interactive-picker code paths, (b) a silent-exit-130 path specifically for interactive-picker cancellation, and (c) a single, consistent "print the full causal chain of context, never a bare stack trace" convention for all other errors.

---

## 9. Design Goals & Non-Goals

zoxide's own README states its purpose in a single sentence, verbatim: *"zoxide is a smarter cd command, inspired by z and autojump. It remembers which directories you use most frequently, so you can 'jump' to them in just a few keystrokes. zoxide works on all major shells."*

Key points to extract from this framing, and from how the README subsequently demonstrates the tool (there is no separate "Goals" or "Non-Goals" section in the README — the philosophy is communicated entirely through this pitch line plus a "Getting started" usage table):

- **Problem solved:** repeatedly typing long, deeply-nested `cd` paths for directories the user visits often. zoxide replaces this with a short fuzzy keyword lookup ranked by actual usage patterns (frecency), not alphabetical or most-recent-only ordering.
- **Explicit lineage, not a from-scratch idea:** zoxide names its direct inspirations — `z` and `autojump` — and, correspondingly, ships first-class `import` support for migrating history *from* both of those tools (plus `fasd`, `z.lua`, `zsh-z`, and `atuin`), rather than asking users to rebuild history from zero.
- **Backward-compatible superset of `cd`, not a replacement mental model.** The README's own usage examples make this explicit: `z ~/foo` is called out as working "like a regular cd command"; `z foo/` does relative-path `cd`; `z ..` goes up one level; `z -` goes to the previous directory — all standard `cd` idioms — *alongside* the novel fuzzy-keyword behavior (`z foo`, `z foo bar`, `z foo /`). The design goal is that a user can `alias cd=z` (or use `z` directly) and lose none of `cd`'s existing behavior while gaining fuzzy jump-by-history on top.
- **Cross-shell support as an explicit differentiator.** The pitch line's closing clause — "zoxide works on all major shells" — is the one concrete claim of superiority the README makes; it backs this with first-class generated integration for nine shells (bash, zsh, fish, POSIX sh/dash/ksh, PowerShell, tcsh, xonsh, elvish, nushell), each with hand-tuned hook/completion implementations rather than one generic script.
- **Interactive disambiguation as an escape hatch, not the default path.** `z` auto-picks the single best (highest-frecency) match with no user interaction; `zi` exists specifically for the case where the top-ranked guess might be wrong, deferring to `fzf` instead of guessing. This two-tier design (automatic-by-default, interactive-on-demand) reflects a goal of keeping the common case fast while not sacrificing correctness for ambiguous queries.
- **Explicit non-goals are not stated in prose anywhere in the README.** The closest things to stated scope boundaries are functional/technical, not philosophical: interactive selection and Space-Tab completion are explicitly gated on having `fzf` installed separately (zoxide does not bundle a fuzzy-finder) and on shell/version support (Space-Tab completion is bash 4.4+/fish/zsh only; unsupported inside Warp, which has its own completion system). The tool does not claim to compete on raw matching sophistication (the keyword matcher is a straightforward right-to-left substring algorithm, not fuzzy/edit-distance matching) — sophistication is deferred to `fzf` for interactive cases.
- **No stated non-goal regarding scope creep** (e.g. zoxide does not claim to avoid becoming a general file-navigation tool, a session manager, etc.) — its scope is implicitly bounded by what's in the CLI surface itself: tracking directories, querying them, and generating shell glue. There is no bookmark system, no per-project config, no multi-machine sync — none of these are mentioned as either goals or explicit non-goals; they are simply absent from the feature set.

---

## 10. Build, Dependency & CI/Release Architecture

This section documents zoxide's own build/dependency/release engineering — useful context for `zjump` if it wants to mirror the packaging surface (shells supported, completions, man pages, cross-platform releases) even though the implementation language differs.

### Key runtime dependencies and their purpose

- **`clap`** — the entire CLI surface (all six subcommands, all flags/enums) is defined via derive macros in one source file, giving a single source of truth for argument parsing, `--help` generation, and validation.
- **`askama`** — a compile-time template engine used to render the nine shell-init scripts (`templates/*.txt`, one per shell) at `zoxide init` time.
- **`bincode`** + **`serde`** — the on-disk database (de)serialization format, see §7.
- **`ouroboros`** — enables the zero-copy self-referential struct that lets deserialized directory paths borrow directly from the loaded file buffer instead of being copied.
- **`glob`** — compiles/matches `_ZO_EXCLUDE_DIRS` patterns.
- **`dirs`** — cross-platform standard-directory lookup (data dir default, home dir for various importers).
- **`dunce`** — Windows path canonicalization without UNC (`\\?\`) prefixes.
- **`fastrand`** — generates random temp-file suffixes for atomic writes.
- **`time`** — parses atuin's textual timestamps during import.
- **`which`** (Windows-only) — resolves `fzf.exe` on `PATH` explicitly, to avoid an unsafe implicit current-directory search that Windows process creation otherwise performs.
- **`clap_complete`, `clap_complete_fig`, `clap_complete_nushell`** (build-time only) — generate shell completion files at compile time; see below.

### Compile-time completion generation (`build.rs`)

zoxide's build script re-parses the same `clap`-derived CLI definition the binary itself uses (by textually including the CLI source file) and, on every `cargo build`/`cargo check`, regenerates shell completion files for **seven** shells (Bash, Elvish, Fig, Fish, Nushell, PowerShell, Zsh) directly into a `contrib/completions/` directory **inside the source tree** — not into Cargo's usual build-output directory. Because the generated files live in the package directory itself, the build script scopes its "rerun if changed" watch to only `build.rs`/`src/`/`templates/`/`tests/` (deliberately excluding `contrib/`), to avoid an infinite rebuild loop where the script's own output would otherwise look like a source change on every subsequent build. The generated completion files are committed to source control, not treated as ignorable build artifacts.

### Release profile

The release build profile enables full link-time optimization, a single codegen unit (maximizing cross-module optimization at the cost of build time), stripped debug symbols, and no embedded debug info — standard settings for producing small, fast release binaries. On Windows MSVC targets specifically, the C runtime is statically linked so the resulting executable has no external `vcruntime` DLL dependency.

### CI and release pipeline

- **Continuous integration** runs lint and test on a single Linux runner (via a pinned, reproducible Nix shell environment), covering formatting, clippy lints, minimum-supported-Rust-version verification, unused-dependency checks, man-page linting, Markdown linting, shell-script linting/formatting of the repo's own install script, and the full Cargo test suite (including the feature-gated shell/template tests — see §11).
- **Release builds** cross-compile for **twelve** target platforms in one workflow: six Linux musl targets (x86_64, ARM/ARMv7 hardfloat, AArch64, i686, RISC-V64), two Android targets (AArch64, ARMv7), two macOS targets (x86_64, AArch64/Apple Silicon), and two Windows MSVC targets (x86_64, AArch64) — each producing a packaged archive (`.tar.gz` on Unix, `.zip` on Windows) bundling the compiled binary, changelog, license, README, man pages, and the full completions directory. A subset of the Linux targets additionally produce a `.deb` package.
- **Publishing** to the crates.io registry is a separate, tag-triggered workflow (pushing a `v*.*.*` tag), decoupled from the draft-GitHub-release workflow above (which instead triggers off a commit-message convention on the main branch). A further workflow republishes the Windows package to the WinGet package manager whenever a GitHub release is published.

### Packaging (`.deb`) subset

Only **three** of the seven generated completion files — Bash, Fish, and Zsh — are actually packaged into the `.deb` (installed to the OS's standard bash-completion/fish-vendor-completion/zsh-vendor-completion directories); Elvish, Fig, Nushell, and PowerShell completions are omitted from the `.deb` specifically, though all seven are still included in the generic release tarball/zip. Man pages and standard doc files (README/CHANGELOG/LICENSE) are also packaged.

### Man pages — a real documentation gap

Six man pages are shipped, one per top-level subcommand except `edit`: `add`, `import`, `init`, `query`, `remove`, plus the top-level `zoxide.1`. **There is no `edit` man page**, even though `zoxide edit` is a real, non-hidden, fully-functional top-level subcommand. This is a genuine documentation gap upstream, not a naming mismatch — worth being aware of if `zjump` intends to mirror zoxide's documentation set rather than improve on it.

---

## 11. Testing Strategy (upstream project)

For reference when designing `zjump`'s own test suite:

- **Unit tests** live inline in the source (in `#[cfg(test)]`-gated modules alongside the code they test), not in a separate library target — zoxide is a binary-only crate, so these run via the binary's own test harness (`cargo test`/`cargo nextest run`, which builds and runs every target), not via a library-specific test invocation. The core database-mutation logic (`add`/`remove` round-tripping through save-then-reopen) and the keyword-matching algorithm (a table of roughly a dozen parametrized keyword/path/expected-match cases covering case-folding, path-component-boundary anchoring, and keyword-overlap rejection) are covered this way, unconditionally — no special build feature is required for these.
- **Shell-template and completion tests** are integration-style: they render each shell's init script (or read its pre-generated completion file) and actually execute/lint it against a **real installed interpreter or linter** for that shell — real interpreters for bash, zsh, fish (plus its formatter), dash, tcsh, elvish, nushell, PowerShell, and xonsh (plus Python-ecosystem formatter/type-checker/linter tools for the xonsh scripts specifically), plus general-purpose shell linters/formatters for the POSIX-family shells. These are gated behind a dedicated, off-by-default build feature so that an ordinary test run (without every one of roughly fifteen external interpreters/linters installed) doesn't fail — the project provides a pinned, reproducible dev-shell environment that supplies exactly this toolset, and CI runs the full gated suite inside that environment. Every combination of the four init-time options (jump-command prefix on/off, all three hook modes, echo on/off, symlink-resolution on/off — a 24-way matrix) is exercised per shell/tool combination, so template correctness is checked across the full option space, not just the defaults.
- **Takeaway for `zjump`:** a comparable Go test suite should (a) keep fast, dependency-free unit tests for the DB/scoring/matching logic runnable unconditionally in the default `go test` path, and (b) push any test that shells out to real interpreters/linters (shell template rendering, completion-file validity) behind an explicit opt-in build tag or test flag, so that ordinary contributors without a full multi-shell environment installed aren't blocked from running the default test suite.

---

## 12. Appendix: Target Command Parity for `zjump`

This checklist enumerates the full zoxide CLI surface — commands, subcommands, flags, environment variables, and load-bearing internal behavior — as a parity-tracking scaffold for the future Go implementation. It is an enumeration only; no Go CLI design decisions are made here.

### Top-level

| Command/Flag | Purpose | Status |
|---|---|---|
| `zoxide` (bare invocation help) | Top-level `--help`/`--version`, dispatch to subcommands | Done (`zjump --help`; bare `zjump` prints usage) |
| `--version` (propagated to all subcommands) | Print version | Done (`-V`/`--version`) |
| Per-subcommand `-h`/`--help` | Print command-specific usage, flags, and environment variables | Done (v0.4.0) |

### `add`

| Command/Flag | Purpose | Status |
|---|---|---|
| `add <PATHS>...` | Add/increment directories in the database | Done (R-ADD-1) |
| `add -s, --score <SCORE>` | Custom increment amount (default 1.0) | Done (R-ADD-2) |
| Exclude-char filtering (`\n`, `\r`) | Silently skip paths containing these characters | Done (R-ADD-3) |
| `_ZO_EXCLUDE_DIRS`-based skip | Silently skip paths matching exclude globs | Done (as `_ZJUMP_EXCLUDE_DIRS`, R-ADD-4) |
| "not a directory" validation | Error on non-directory paths | Done (R-ADD-5) |
| Symlink resolution toggle | Canonicalize vs. lexical resolve based on `_ZO_RESOLVE_SYMLINKS` | Done (R-ADD-6) |
| Post-add aging pass | Rescale/prune ranks when `_ZO_MAXAGE` total exceeded | Done (R-ADD-8) |

### `query`

| Command/Flag | Purpose | Status |
|---|---|---|
| `query [KEYWORDS]...` | Search/print best-matching directory | Done (R-QRY-1) |
| `query -a, --all` | Include unavailable (nonexistent) directories; disable existence filter | Done (R-QRY-6) |
| `query -i, --interactive` | Interactive `fzf`-based selection | Done (R-QRY-4) |
| `query -l, --list` | List all matches | Done (R-QRY-3) |
| `query -s, --score` | Print scores alongside results | Done (R-QRY-5) |
| `query --exclude <path>` | Exclude a specific path from results — separate mechanism from `_ZO_EXCLUDE_DIRS`, no deletion side effect | Done (R-QRY-7) |
| `query --base-dir <path>` | Restrict results to a directory subtree | Done (R-QRY-8) |
| Keyword matcher (right-to-left substring, last-keyword component anchoring) | Core fuzzy match algorithm | Done (R-MATCH-1/2/3; parity table ported) |
| Frecency score formula (4x/2x/0.5x/0.25x decay buckets) | Ranking/ordering of results | Done (R-MATCH-4) |
| Lazy deletion on `_ZO_EXCLUDE_DIRS` glob match (distinct from `--exclude`) | Purge excluded entries during query | Done (R-QRY-10a) |
| Lazy deletion on stale+nonexistent (TTL ~90 days) | Purge long-dead entries during query | Done (R-QRY-10b) |
| "no match found" / "you are already in the only match" errors | Error-case parity | Done (R-QRY-1/2) |
| Unconditional DB rewrite on every query (dirty-on-sort behavior) | Persistence-timing parity (may be revisited in Go design) | **Deviation D-4** — zjump rewrites only when actually dirty |

### `remove`

| Command/Flag | Purpose | Status |
|---|---|---|
| `remove [PATHS]...` | Remove directories from the database | Done (R-RM-1) |
| Exact-match then resolved-path-match fallback | Two-stage removal lookup | Done (R-RM-2) |
| "path not found in database" error | Error-case parity | Done (R-RM-3) |
| Order-disturbing O(1) removal semantics | Parity for downstream ordering effects (see `edit`) | Done (swap-remove) |

### `init`

| Command/Flag | Purpose | Status |
|---|---|---|
| `init <SHELL>` (bash/elvish/fish/nushell/posix\|ksh/powershell/tcsh/xonsh/zsh) | Emit shell integration script | Done for bash + zsh (R-INIT-1); other shells out of scope (N-2) |
| `init --no-cmd` / `--no-aliases` | Suppress `z`/`zi` command generation | Done (R-INIT-2) |
| `init --cmd <CMD>` | Configure jump-command prefix (default `z`) | Done (R-INIT-3) |
| `init --hook <HOOK>` (none/prompt/pwd) | Configure tracking hook mode | Done (R-INIT-4) |
| Per-shell hook implementation: bash (`PROMPT_COMMAND`, emulated pwd-diff via cached `$__zoxide_oldpwd`) | Shell-specific hook wiring | Done (R-INIT-4) |
| Per-shell hook implementation: zsh (`precmd_functions`/`chpwd_functions`) | Shell-specific hook wiring | Done (R-INIT-4) |
| Per-shell hook implementation: fish (`fish_prompt`/`PWD` variable events) | Shell-specific hook wiring | Out of scope (N-2) |
| Per-shell hook implementation: posix (`$PS1` injection; pwd mode unsupported) | Shell-specific hook wiring | Out of scope (N-2) |
| Per-shell hook implementation: powershell/tcsh/xonsh/elvish/nushell | Shell-specific hook wiring | Out of scope (N-2) |
| `__zoxide_pwd`-equivalent helper (resolves `-L`/`-P`, Windows path translation) | Correct pwd resolution feeding into the hook | Done (`-L`/`-P`; no Windows translation — **deviation D-5**) |
| Tab-completion: bash (Space-Tab, escape-sequence redraw trick, bash ≥4.4) | Interactive completion | Done (R-INIT-9) |
| Tab-completion: zsh (`compdef` + ZLE widget) | Interactive completion | Done (R-INIT-9) |
| Tab-completion: fish (`commandline --function repaint execute`) | Interactive completion | Out of scope (N-2) |
| `_ZO_ECHO` wiring into generated `cd` wrapper | Echo matched directory before navigating | Done (as `_ZJUMP_ECHO`, R-INIT-8) |
| `_ZO_RESOLVE_SYMLINKS` wiring into generated `pwd` helper | Symlink-aware pwd reporting | Done (as `_ZJUMP_RESOLVE_SYMLINKS`, R-INIT-7) |

### `edit`

| Command/Flag | Purpose | Status |
|---|---|---|
| `edit` (no subcommand) — interactive fzf session | Browse/mutate database interactively | Done (R-EDIT-1) |
| `edit increment <path>` (hidden) | Bump an entry's rank by +1.0 | Done (R-EDIT-2) |
| `edit decrement <path>` (hidden) | Drop an entry's rank by −1.0 (floored at 0) | Done (R-EDIT-2) |
| `edit delete <path>` (hidden) | Remove an entry | Done (R-EDIT-2) |
| `edit reload` (hidden) | No-op, used to re-dump current list | Done (R-EDIT-2) |
| Fixed `fzf` key-binding set (ctrl-r/ctrl-d/ctrl-w/ctrl-s/enter:abort/etc.) | Interactive UI wiring | Done (R-EDIT-3) |
| Always-on preview pane (independent of `_ZO_FZF_OPTS`) | UI parity | Done (R-EDIT-3) |
| `increment`/`decrement` insert-if-missing fallback (sets `last_accessed = now` only in that branch) | Edge-case parity | Done (R-EDIT-4) |
| Session-scoped sort staleness after `ctrl-d` deletes (`swap_remove` ordering not re-sorted mid-session) | Behavioral-quirk parity (may be deliberately fixed in Go design instead) | **Deviation D-3** — zjump re-sorts after every mutating reload |

### `import`

| Command/Flag | Purpose | Status |
|---|---|---|
| `import atuin` | Import from atuin (subprocess-based) | Out of scope (N-1) |
| `import autojump` | Import from autojump (sigmoid rank transform) | Out of scope (N-1) |
| `import fasd` | Import from fasd | Out of scope (N-1) |
| `import z` | Import from z | Out of scope (N-1) |
| `import z.lua` | Import from z.lua (with Fish-path fallback) | Out of scope (N-1) |
| `import zsh-z` | Import from zsh-z | Out of scope (N-1) |
| `import --merge` | Allow import into a non-empty database | Out of scope (N-1) |
| Non-empty-DB-without-`--merge` guard | Safety-rail parity | Out of scope (N-1) |
| Per-record non-fatal error logging | Malformed-row tolerance | Out of scope (N-1) |
| Unconditional insert + post-import dedup (sum rank, max last_accessed) | Duplicate-handling semantics | Engine present (`AddUnchecked`+`Dedup`), unused this scope (F-3) |
| Post-import aging pass | Rank rescaling/pruning after bulk import | Out of scope (N-1) |
| `_ZO_EXCLUDE_DIRS` filtering during import | Skip excluded paths on import | Out of scope (N-1) |

### Environment variables

| Command/Flag | Purpose | Status |
|---|---|---|
| `_ZO_DATA_DIR` | Database file location override (absolute-path validation applies unconditionally, not just when explicitly set) | Done (as `_ZJUMP_DATA_DIR`, R-ENV-1; prefix change is **deviation D-2**) |
| `_ZO_ECHO` | Echo matched directory before navigating | Done (as `_ZJUMP_ECHO`, R-ENV-2) |
| `_ZO_EXCLUDE_DIRS` | Glob-based exclusion list (distinct from `--exclude`) | Done (as `_ZJUMP_EXCLUDE_DIRS`, R-ENV-3) |
| `_ZO_FZF_OPTS` | Custom `fzf` args (query-interactive only, never `edit`) | Done (as `_ZJUMP_FZF_OPTS`, R-ENV-4) |
| `_ZO_MAXAGE` | Aging/pruning threshold | Done (as `_ZJUMP_MAXAGE`, R-ENV-5) |
| `_ZO_RESOLVE_SYMLINKS` | Symlink resolution toggle | Done (as `_ZJUMP_RESOLVE_SYMLINKS`, R-ENV-6) |

### Database/persistence semantics (cross-cutting, not a CLI flag but load-bearing for parity)

| Command/Flag | Purpose | Status |
|---|---|---|
| Atomic write (temp file + rename) | Crash-safe database persistence | Done (R-DB-2) |
| On-disk binary format (u32 version header + fixed-width little-endian encoding + 32 MiB deserialize size guard; see §7) | Byte-level compatibility target, or basis for a deliberately different Go-native format | Done — **deviation D-1** (zjump-native `ZJDB` magic + version, 32 MiB guard; not `db.zo`-compatible) |
| Frecency score formula (decay buckets) | Core ranking algorithm | Done (R-MATCH-4) |
| Aging/pruning algorithm (`0.9 × max_age / total_age` scaling) | Long-term database size control | Done (R-DB-4) |
| Deduplication (path-sorted, adjacent-merge, sum rank / max last_accessed) | Used by `import` | Done — retained for future `import` (R-DB-5, F-3) |
| Rank floor at 0.0 (`add`/`add_update` paths only, not `import`'s unconditional-insert path) | Invariant parity | Done |

### Process/error-handling conventions (cross-cutting)

| Command/Flag | Purpose | Status |
|---|---|---|
| Silent exit 0 on broken-pipe stdout writes, in every output-producing subcommand (not just interactive ones) | Correct behavior under Unix pipelines (e.g. `zjump query --list \| head`) | Done (R-ERR-1; SIGPIPE ignored so EPIPE surfaces) |
| Silent exit 130 on interactive-picker cancel (Ctrl-C/Esc, or `edit`'s Enter-as-abort) | Interactive-session parity | Done (R-ERR-2) |
| Full causal-chain error printing (`tool: <full context chain>`), no stack trace | Error-reporting convention | Done (R-ERR-3; `%w` chains printed via `%v`) |
| Minimum supported `fzf` version enforcement/documentation (v0.51.0 upstream) | Interactive-mode compatibility | Documented (R-FZF-6; not enforced at runtime, matching upstream) |
