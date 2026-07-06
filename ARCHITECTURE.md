# Architecture

This document describes the architecture of [zoxide](https://github.com/ajeetdsouza/zoxide), a Rust CLI tool, as reference material for the `zjump` team reimplementing its functionality in Go. All claims below are grounded in direct source inspection of the zoxide repository (file/line citations are preserved from the verification passes so claims can be re-checked against source).

## 1. Overview

zoxide is a single Rust binary (crate name `zoxide`, no `[lib]` target — it is binary-only, built from `src/main.rs` with modules `cmd`, `config`, `db`, `error`, `import`, `shell`, `util`). There is no daemon or background process: every invocation is a fresh, short-lived process that opens a database file, does some work, and (usually) rewrites the file before exiting.

Note on static linking: zoxide is **not** uniformly statically linked. The only static-linking configuration in the repo is `.cargo/config.toml`'s `rustflags = ["-C", "target-feature=+crt-static"]`, scoped specifically to `cfg(all(windows, target_env = "msvc"))`, which statically links the C runtime so Windows MSVC builds carry no `vcruntime` DLL dependency. Separately, 5 of the release pipeline's 6 Linux targets are built against `musl` libc (via `cross`), which is conventionally static, but this is a property of the target triple/toolchain, not an explicit project-wide static-linking directive. Ordinary builds for other targets (e.g. glibc Linux, macOS) have no special static-linking configuration.

The system has three cooperating parts:

1. **CLI front-end** — a `clap`-derived argument parser (`src/cmd/cmd.rs`) defining six subcommands: `add`, `edit`, `import`, `init`, `query`, `remove`. Dispatch is a flat `match` in `src/cmd/mod.rs` over a `Cmd` enum, each variant delegating to a `Run` trait impl in its own `src/cmd/*.rs` file. See §6 for behavioral detail on each subcommand, including `remove`.
2. **Local frecency database** — a single flat file, `db.zo`, holding a `bincode`-serialized `Vec<Dir>` (path + rank + last-accessed timestamp per entry). All ranking, matching, aging, and deduplication logic operates on this in-memory vector; it is read fully into memory and atomically rewritten on save.
3. **Shell-generated integration scripts** — `zoxide init <shell>` renders an [Askama](https://github.com/rinja-rs/askama) compile-time template (one `.txt` file per shell in `templates/`) to stdout. The user's shell `eval`s or sources this output, which defines `z`/`zi` wrapper functions, a directory-change "hook" that shells out to `zoxide add`, and (for bash/zsh/fish) tab-completion glue. The shell script is the only "always-running" part of the system; the compiled binary itself never persists between invocations.

Interactive fuzzy selection (`zoxide query -i`, `zoxide edit`) is delegated entirely to the external `fzf` binary via subprocess — it is not bundled or reimplemented in zoxide.

Data flow for the common case (`z foo`):
```
shell function `z` → zoxide query --exclude <pwd> -- foo   (subprocess)
                    → reads db.zo, ranks/filters candidates, prints best match path
shell `cd`s into that path
shell hook fires (Prompt or Pwd mode) → zoxide add -- <new pwd>  (subprocess)
                    → reads db.zo, bumps rank/last_accessed, ages/prunes, atomically rewrites db.zo
```

## 2. Core Dependencies

### Runtime (`[dependencies]`)

| Crate | Version | Why it's used |
|---|---|---|
| `anyhow` | 1.0.32 | Ergonomic error handling (`Result`, `Context`, `bail!`, `ensure!`) used across ~18 files; `main.rs` prints the full `{:?}` causal chain of context layers on failure. |
| `askama` | 0.16.0 (`default-features=false`, features `derive`,`std`) | Compile-time template engine that renders the 9 shell-init scripts (`templates/*.txt`, `escape="none"`) from an `Opts` struct (`cmd`, `hook`, `echo`, `resolve_symlinks`). |
| `bincode` | 1.3.1 declared, **1.3.3 locked** | Binary (de)serialization of the on-disk database. Write side uses the top-level free functions (implicitly fixint-encoded); read side explicitly chains `.with_fixint_encoding().with_limit(32<<20)` so the two are byte-compatible and reads are bounded to 32 MiB. |
| `clap` | 4.3.0 (`derive`) | Entire CLI surface: the `Cmd` enum and all subcommand structs live in one file, `src/cmd/cmd.rs`, via `#[derive(Parser)]`/`#[clap(...)]`. Also derived directly (`#[derive(clap::Args, ...)]`) on each importer's options struct (`src/import/{atuin,autojump,fasd,z,z_lua,zsh_z}.rs`) since those are spliced into the `import` subcommand tree. |
| `color-print` | 0.3.4 | `color_print::cstr!()` builds a custom ANSI-colored clap help template (`HelpTemplate` in `cmd.rs`) appending an "Environment variables" section to every subcommand's `--help`. |
| `dirs` | 6.0.0 | Cross-platform standard-directory lookup: `dirs::data_local_dir()` for the default DB location, `dirs::home_dir()` used by five of the six importers. |
| `dunce` | 1.0.1 | Windows path canonicalization that avoids UNC (`\\?\`) prefixes; sole use is `dunce::canonicalize()` for `_ZO_RESOLVE_SYMLINKS`-enabled `add` (see §6 for how this relates to the separate, purely-lexical `resolve_path` helper). |
| `fastrand` | 2.0.0 | Fast non-cryptographic RNG; generates the random 12-char suffix for temp-file names used in the atomic-write path. |
| `glob` | 0.3.0 | Glob pattern matching for `_ZO_EXCLUDE_DIRS` parsing (`config.rs`) and lazy exclusion during query streaming (`db/stream.rs`). |
| `ouroboros` | 0.18.3 | `#[self_referencing]` macro powering `Database`, letting `dirs: Vec<Dir<'this>>` hold string slices that borrow directly from the struct's own owned `bytes: Vec<u8>` field — avoids a per-entry string copy on load. |
| `serde` | 1.0.116 (`derive`) | `#[derive(Serialize, Deserialize)]` on `Dir<'a>`; `#[serde(borrow)]` on its `Cow<'a, str>` path field enables the zero-copy deserialization above, paired with bincode. |
| `time` | 0.3.47 (`parsing`,`macros`,`std`) | Parses atuin's `YYYY-MM-DD HH:MM:SS` UTC timestamp format during import (`time::macros::format_description!`, `PrimitiveDateTime::parse`). |
| `which` (Windows-only target dep) | 8.0.2 | Resolves `fzf.exe`'s full path on Windows, avoiding `CreateProcess`'s implicit unsafe search of the current working directory. |

### Build (`[build-dependencies]`)

| Crate | Version | Why it's used |
|---|---|---|
| `clap` | 4.3.0 (`derive`) | `build.rs` textually includes `src/cmd/cmd.rs` via `#[path=...] mod cmd;` and calls `Cmd::command()` to drive completion generation. |
| `clap_complete` | 4.5.50 | Generates Bash/Elvish/Fish/PowerShell/Zsh completion scripts at build time. |
| `clap_complete_fig` | 4.5.2 | Generates Fig (TypeScript) completions. |
| `clap_complete_nushell` | 4.5.5 | Generates Nushell completions. |
| `color-print` | 0.3.4 | Required transitively because `build.rs` compiles `cmd.rs` verbatim, and `cmd.rs` calls `color_print::cstr!()`; not called directly by `build.rs` itself. |

### Dev/test (`[dev-dependencies]`)

| Crate | Version | Why it's used |
|---|---|---|
| `assert_cmd` | 2.0.0 | Spawns and asserts on real subprocesses (`bash`, `fzf`, etc.) in tests. |
| `rstest` | 0.26.0 (no default features) | Parameterized `#[case(...)]` tests, e.g. 14 cases for the keyword-matching filter. |
| `rstest_reuse` | 0.7.0 | Shared `#[template]` fixture reused via `#[apply(opts)]` across 20 shell-template test functions, expanding a 2×3×2×2 = 24-case option matrix into 480 generated tests. |
| `tempfile` | 3.15.0 | Temp dirs/files for DB and shell-init tests. |

### `[features]`

`default = []`; `nix-dev = []` — an empty marker feature gating all tests that shell out to real interpreters/linters (bash, zsh, fish, dash, elvish, nu, pwsh, tcsh, xonsh, shellcheck, shfmt, black, mypy, pylint), provided by the Nix dev shell (`shell.nix`). Without `--features nix-dev`, `cargo test` runs only 16 tests (the pure-Rust DB tests); with it, 480 + 4 additional tests run.

### MSRV

`Cargo.toml` declares `rust-version = "1.88.0"` as the crate's minimum supported Rust version. This is not just informational — it's enforced in CI (`cargo msrv verify`, part of `just lint`, see §10), so a genuine build/tooling constraint the Go team should note as the parity bar if `zjump` intends to track zoxide's minimum-toolchain policy.

## 3. Data Storage

### File location

Path is always `$data_dir/db.zo`. `data_dir` resolution (`src/config.rs::data_dir`):
- `_ZO_DATA_DIR` env var if set (must be an absolute path, else error), else
- `dirs::data_local_dir()` joined with `"zoxide"` — e.g. `~/.local/share/zoxide` on Linux (XDG), the platform-appropriate local-data directory on macOS/Windows.

The joined path is then best-effort `fs::canonicalize`d (falls back to the uncanonicalized path if the file doesn't exist yet, e.g. on first run). If the data directory doesn't exist, it's created with `fs::create_dir_all`; the `db.zo` file itself is not created until the first `save()` that actually has dirty data.

### On-disk byte layout

Format version constant: `Database::VERSION: u32 = 3`. Serialization is `bincode` (locked at 1.3.3) using **fixint encoding** (fixed-width, not varint) and **little-endian** byte order — not native endianness — on both the write side (bincode's top-level free functions default to fixint) and the read side (explicit `.with_fixint_encoding()`). Struct fields carry no name/type tags; they are written positionally with no framing.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 4 | `version: u32` (LE) | must equal `3`, else load fails with "unsupported version" |
| 4 | 8 | `len: u64` (LE) | count of `Dir` entries |
| 12 | 8 | `path_len: u64` (LE) | byte length of entry 0's path |
| 20 | `path_len` | `path` | raw UTF-8, no NUL terminator |
| 20+path_len | 8 | `rank: f64` (LE IEEE-754) | entry 0's rank |
| +8 | 8 | `last_accessed: u64` (LE) | entry 0's last-accessed epoch seconds |
| ... | | (repeat per entry) | 24 bytes fixed overhead + `len(path)` per entry |

Deserialization enforces a hard **32 MiB** (`32 << 20`) size limit up front so corrupt/oversized input fails fast rather than producing obscure bincode errors. If the file is shorter than the version field (4 bytes), loading bails with "corrupted data".

The file is **not memory-mapped** (no mmap dependency anywhere in the crate); it is read fully into a heap `Vec<u8>` via `std::fs::read`. Zero-copy is achieved differently: because `Dir::path` is `Cow<'a, str>` with `#[serde(borrow)]`, and `Database` is a self-referential (`ouroboros`) struct whose `dirs: Vec<Dir<'this>>` borrows from its own `bytes: Vec<u8>` field, bincode's slice-reader deserializes each valid-UTF-8 path as `Cow::Borrowed(&str)` pointing directly into the loaded buffer — no per-entry string allocation.

### In-memory struct definitions

```rust
#[self_referencing]
pub struct Database {
    path: PathBuf,
    bytes: Vec<u8>,
    #[borrows(bytes)]
    #[covariant]
    pub dirs: Vec<Dir<'this>>,
    dirty: bool,
}

pub type Rank = f64;
pub type Epoch = u64;

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Dir<'a> {
    #[serde(borrow)]
    pub path: Cow<'a, str>,
    pub rank: Rank,
    pub last_accessed: Epoch,
}

pub struct DirDisplay<'a> {
    dir: &'a Dir<'a>,
    now: Option<Epoch>,
    separator: char,
}

pub struct Stream<'a> {
    db: &'a mut Database,
    idxs: Rev<Range<usize>>,
    options: StreamOptions,
}

pub struct StreamOptions {
    now: Epoch,
    keywords: Vec<String>,
    exclude: Vec<Pattern>,
    exists: bool,
    resolve_symlinks: bool,
    ttl: Epoch,
    base_dir: Option<String>,
}
```

### Atomic write mechanism

`Database::save()` is a no-op unless `dirty()`. When dirty, `util::write(path, contents)` performs:

1. Create a randomly-named temp file **in the same directory** as the target (`"tmp_" + 12 alphanumeric chars`, `fastrand::alphanumeric()`), using `OpenOptions::create_new(true)` for collision-safe atomic creation, retried up to 5 times on `AlreadyExists`.
2. `set_len` + `write_all` the full new contents.
3. On Unix, best-effort `fchown` the temp file to the original file's existing uid/gid, so a rewrite doesn't silently change ownership.
4. `sync_all()` — primarily to force any deferred write errors to surface here (rather than being silently dropped when the handle later closes), incidentally also flushing to durable storage.
5. `rename(tmp_path, path)` — a single unretried call on non-Windows (atomic by OS guarantee); retried up to 5 times on `PermissionDenied` on Windows, where rename is only best-effort atomic.
6. On any failure in the sequence, the temp file is deleted before the error propagates.

**Important caveat:** `sort_by_path`/`sort_by_score` unconditionally set `dirty = true` regardless of whether ordering actually changed, and `Stream::new` (used by every `zoxide query`) calls `sort_by_score` on construction. Combined with `cmd/query.rs`'s `self.query(&mut db).and(db.save())` (which evaluates `db.save()` eagerly regardless of the left operand, since `Result::and` takes its argument by value, not a closure), **essentially every `zoxide query` invocation performs a full atomic rewrite of `db.zo`**, not just ones that add/prune entries. The same applies to interactive `zoxide edit`.

## 4. Frecency Algorithm

### Scoring formula (`Dir::score`)

```rust
pub fn score(&self, now: Epoch) -> Rank {
    let duration = now.saturating_sub(self.last_accessed);
    if duration < HOUR {       // < 3_600 s
        self.rank * 4.0
    } else if duration < DAY {  // < 86_400 s
        self.rank * 2.0
    } else if duration < WEEK { // < 604_800 s
        self.rank * 0.5
    } else {
        self.rank * 0.25
    }
}
```

Constants (seconds): `SECOND=1`, `MINUTE=60`, `HOUR=3600`, `DAY=86400`, `WEEK=604800`, `MONTH=2_592_000` (30 days; used only by the query TTL, not by `score()`). `duration` uses `saturating_sub`, so future-dated (clock-skewed) `last_accessed` values land in the highest-weight bucket.

This decayed `score()` — not the raw `rank` field — is what's shown by `--score`, what drives sort order (`sort_by_score`, ascending, via `f64::total_cmp`, then iterated in reverse so the highest score comes first), and what's streamed to fzf. Display is separately clamped to `[0.0, 9999.0]` and formatted `{score:>6.1}` (fixed 6-char field).

### Rank accumulation

`rank` is the raw frecency counter, independent of time decay. Three mutators, each with distinct semantics:

| Function | Used by | Behavior | Clamped to ≥0? |
|---|---|---|---|
| `add(path, by, now)` | `zoxide edit increment/decrement` | On existing path: `rank += by`. Does **not** touch `last_accessed`. On new path: inserts with `last_accessed = now`. | Yes |
| `add_update(path, by, now)` | `zoxide add` (via shell hook) | On existing path: `rank += by` **and** `last_accessed = now`. On new path: inserts. | Yes |
| `add_unchecked(path, rank, last_accessed)` | `zoxide import` only | Always pushes a **new** entry, no existing-path check (duplicates expected, cleaned up by `dedup()` afterward). | **No** — imported entries can carry negative rank if the source data is adversarial/malformed. |

Increment values (`by`) at each call site: `zoxide add [--score N]` uses `N` or default `1.0`; `zoxide edit increment` hardcodes `1.0`; `zoxide edit decrement` hardcodes `-1.0` (floors at rank `0.0` due to the clamp, does not auto-delete).

### Aging/culling formula (`Database::age`)

```rust
pub fn age(&mut self, max_age: Rank) {
    let total_age: Rank = dirs.iter().map(|dir| dir.rank).sum();
    if total_age > max_age {
        let factor = 0.9 * max_age / total_age;
        for idx in (0..dirs.len()).rev() {
            dirs[idx].rank *= factor;
            if dirs[idx].rank < 1.0 {
                dirs.swap_remove(idx);
            }
        }
    }
}
```

- **Trigger condition:** sum of *every* entry's raw `rank` exceeds `max_age`.
- **`max_age` source:** `_ZO_MAXAGE` env var parsed as `u32` → `f64`, default **`10_000.0`**.
- **Decay:** a single global scalar `factor = 0.9 * max_age / total_age` applied to every entry, deliberately undershooting so the new total settles near `0.9 * max_age`, giving headroom before the next trigger.
- **Culling:** any entry whose post-decay rank falls below `1.0` is removed via `swap_remove` (safe because iteration is in reverse index order).
- **Callers:** after `zoxide add` (only if `db.dirty()`), and after `zoxide import` completes (immediately after `dedup()`).

### Deduplication (`Database::dedup`)

Sorts entries by path (lexicographic, byte-wise `Cow<str>::Ord`) so equal paths become adjacent, then scans backward merging adjacent duplicates: merged `rank = prev.rank + curr.rank` (summed, not averaged, not re-clamped), merged `last_accessed = max(prev, curr)`, duplicate removed via `swap_remove`. `add`/`add_update` proactively dedup via a linear existing-path check, so duplicates only arise from `add_unchecked` (i.e., only from `zoxide import`), where `dedup()` is the sole call site outside `db/mod.rs` itself.

## 5. Query & Matching

### Candidate ordering

`Stream::new` calls `db.sort_by_score(now)` (ascending) on construction, then iterates database indices via a **reversed** range, so `Stream::next()` yields the highest-scoring (best) match first.

### Filter pipeline (applied per candidate, in this exact order)

1. **`filter_by_keywords`** — see algorithm below; non-matches are simply skipped.
2. **`filter_by_base_dir`** (`--base-dir <path>`) — `Path::new(&dir.path).starts_with(base_dir)`, a component-wise prefix match (so `/foo` does not match `/foobar`). The `base_dir` value is used verbatim, never resolved/canonicalized.
3. **`filter_by_exclude`** (`_ZO_EXCLUDE_DIRS` globs) — if a candidate matches any exclude glob, it is **immediately and permanently removed** from the live in-memory database (`swap_remove`), not merely hidden from this query's results — a lazy-deletion side effect of any query that iterates past it.
4. **`filter_by_exists`** (skipped entirely when `--all` is passed) — checked last because it's the slowest. Uses `fs::symlink_metadata` (no symlink following) if `_ZO_RESOLVE_SYMLINKS=1`, else `fs::metadata` (follows symlinks) — deliberately inverted: if symlinks were resolved at *add* time, the stored path shouldn't itself be re-resolved at query time. If the path is missing/not-a-dir **and** `last_accessed < ttl`, the entry is also lazily `swap_remove`d. `ttl = now - 3*MONTH` (90 days) — currently-missing-but-recently-used directories (e.g. an unmounted drive) are hidden from results but retained in the DB until they've also gone stale.

### Keyword matching algorithm (`filter_by_keywords`)

Case-insensitive (ASCII-fast-path lowercasing), plain substring search (`str::rfind`, not fuzzy/edit-distance), matched **right-to-left**:

```rust
fn filter_by_keywords(&self, path: &str) -> bool {
    let (keywords_last, keywords) = match self.options.keywords.split_last() {
        Some(split) => split,
        None => return true,
    };
    let path = util::to_lowercase(path);
    let mut path = path.as_str();
    match path.rfind(keywords_last) {
        Some(idx) => {
            if path[idx + keywords_last.len()..].contains(path::is_separator) {
                return false;   // last keyword must end within the final path component
            }
            path = &path[..idx];
        }
        None => return false,
    }
    for keyword in keywords.iter().rev() {
        match path.rfind(keyword) {
            Some(idx) => path = &path[..idx],   // truncate leftward, no overlap allowed
            None => return false,
        }
    }
    true
}
```

The **last** query keyword is anchored to the final path component (nothing after its match may contain a path separator). Earlier keywords have no such anchor but must appear, in order, strictly to the left of the previously matched span — so keyword spans cannot overlap and must preserve left-to-right order in the path.

### Query dispatch (`zoxide query`)

- `query_first` (default): first match from the `Stream`; errors `"no match found"` on empty stream, `"you are already in the only match"` if the only match equals `--exclude`.
- `query_list` (`--list`): prints every match.
- `query_interactive` (`--interactive`): streams matches into `fzf`'s stdin as they're pulled from the `Stream`, waits for a selection.

Both `--exclude <path>` (CLI flag, checked in the streaming loop, exact string compare) and `_ZO_EXCLUDE_DIRS` (glob, evaluated inside `Stream::next`, causes permanent deletion) coexist and serve different purposes.

## 6. CLI Command Surface

### Command summary

| Command | Signature | One-line behavior |
|---|---|---|
| `add` | `zoxide add <paths>... [-s/--score N]` | Increments/inserts rank for each path via `add_update`; skips excluded/malformed paths; runs `age()` if dirty; saves. |
| `edit` | `zoxide edit [<hidden subcommand>]` | With no subcommand: sorts by score, saves, launches interactive fzf browser/editor. Hidden subcommands (`increment`/`decrement`/`delete`/`reload`) are invoked internally by fzf's `reload(...)` key bindings to mutate the DB and redraw the list. |
| `import` | `zoxide import <from> [--merge]` | Bulk-imports history from another tool through the shared `import::run()` pipeline (§9). |
| `init` | `zoxide init <shell> [--no-cmd] [--cmd X] [--hook Y]` | Renders the shell-specific Askama template to stdout (§7). |
| `query` | `zoxide query [keywords...] [-a|-i|-l] [-s] [--exclude P] [--base-dir P]` | Finds/ranks matching directories (§5). |
| `remove` | `zoxide remove [paths...]` | Deletes directories from the DB (detailed below). |

### `zoxide remove` — exact-match then lexical-resolve fallback

`remove` is the only one of the six subcommands with no dedicated write-up elsewhere in this document, and it is also the **only subcommand that never calls `util::current_time()`** — it has no clock-error case, since deletion doesn't need "now" for anything.

Behavior, per path argument:
1. Try `db.remove(path)` — an **exact string match** against stored paths (`Vec::position` + `swap_remove`, O(1) but order-disturbing).
2. If that fails, resolve `path` via `util::resolve_path` (see below — purely lexical, no filesystem access, no symlink following) and retry `db.remove(resolved)`.
3. If the resolved path is identical to the original (i.e. it was already absolute, so retrying is pointless) **or** the retry also fails, the command bails with `"path not found in database: {path}"`.

After processing all paths, `db.save()` is called once (subject to the usual dirty-check no-op behavior from §3).

### `resolve_path` vs. `canonicalize` — the two path-normalization primitives

zoxide uses two distinct, non-interchangeable path-normalization helpers in `util.rs`, and understanding the difference is important for a faithful reimplementation:

| Helper | Mechanism | Touches filesystem? | Resolves symlinks? |
|---|---|---|---|
| `util::resolve_path` | Purely lexical: joins a relative path onto the current working directory and normalizes `.`/`..` components as plain string/path-component manipulation | No | No |
| `util::canonicalize` | Wraps `dunce::canonicalize` (which itself wraps the OS canonicalization call, but strips Windows' `\\?\` UNC prefix from the result) | Yes | Yes |

Call sites:
- **`add`**: uses `util::canonicalize` (symlink-resolving) when `_ZO_RESOLVE_SYMLINKS=1`, else falls back to the purely-lexical `util::resolve_path`.
- **`remove`**: always uses `util::resolve_path` (lexical only) as its fallback-match strategy — it never resolves symlinks, regardless of `_ZO_RESOLVE_SYMLINKS`.
- **`query`'s existence filter** (`filter_by_exists`, §5): doesn't call either helper directly, but implements the same symlink-resolution toggle at the metadata layer — `fs::symlink_metadata` (no-follow) vs. `fs::metadata` (follow), chosen by `_ZO_RESOLVE_SYMLINKS`, with the logic deliberately inverted relative to `add` (if paths were resolved at add-time, they shouldn't be re-resolved at query-time).

So `_ZO_RESOLVE_SYMLINKS` is a single logical on/off switch, but it's implemented via three different concrete mechanisms depending on which command is consuming it.

### `SilentExit` and the general quiet-exit mechanism (`main.rs`, `error.rs`)

This is a project-wide error-handling pattern, not something specific to fzf (it is introduced piecemeal via the fzf exit-code table in §8, but the mechanism is broader):

- `main()` unconditionally strips `RUST_LIB_BACKTRACE` and `RUST_BACKTRACE` from the process environment at startup (`unsafe { env::remove_var(...) }`), disabling Rust backtraces regardless of what the user has set.
- It then calls `Cmd::parse().run()`. On `Ok(())` the process exits with `ExitCode::SUCCESS`.
- On `Err(e)`: if `e` downcasts to the crate's `SilentExit { code: u8 }` type, the process exits with that exact code and **prints nothing** — `SilentExit`'s `Display` impl is intentionally a no-op. Otherwise, `"zoxide: {e:?}"` is printed to stderr (anyhow's `Debug` formatting, which renders the *entire* `.context(...)` causal chain, not just the innermost message) and the process exits with `ExitCode::FAILURE`.
- `SilentExit` is raised from two independent situations:
  1. **Any stdout (or fzf-stdin) write hitting a broken pipe** — a shared `BrokenPipeHandler::pipe_exit(device)` trait method maps `io::ErrorKind::BrokenPipe` to `SilentExit{code: 0}` (any other I/O error instead gets wrapped with `"could not write to {device}"` context). This covers ordinary piping scenarios (e.g. `zoxide query --list | head`).
  2. **fzf exiting with status 130** (user pressed Ctrl-C/Esc during `query -i` or `edit`) — `FzfChild::wait()` maps this to `SilentExit{code: 130}`, so the outer zoxide process also exits silently with 130, matching SIGINT convention (128+2).

### Environment variable reference

All six variables are read via `src/config.rs` and are also summarized (abbreviated) in every subcommand's `--help` output via the custom clap help template.

| Variable | Config function | Purpose | Default / validation |
|---|---|---|---|
| `_ZO_DATA_DIR` | `data_dir()` | Path for the `db.zo` database file (§3) | `dirs::data_local_dir()/zoxide` if unset; must be an absolute path if set (else error) |
| `_ZO_ECHO` | `echo()` | Print the matched directory before navigating to it | `false` unless the value is exactly the string `"1"` |
| `_ZO_EXCLUDE_DIRS` | `exclude_dirs()` | Glob patterns excluded from `add`, `import`, and (lazily, with permanent deletion) `query` | If unset: a single pattern matching the home directory, glob-escaped so it matches the home dir **literally and only** (not subdirectories). If set: OS path-list of glob patterns; invalid UTF-8/glob syntax errors out. |
| `_ZO_FZF_OPTS` | `fzf_opts()` | Custom fzf flags — **read only by `query -i`**, never by `edit` | `None` if unset (built-in args + preview used instead); if set, becomes the child's `FZF_DEFAULT_OPTS` verbatim and disables the preview pane as a side effect |
| `_ZO_MAXAGE` | `maxage()` | Aging ceiling — total raw-rank sum after which entries are rescaled/pruned (§4) | `10_000.0` if unset; else parsed as `u32` then cast to `f64` |
| `_ZO_RESOLVE_SYMLINKS` | `resolve_symlinks()` | Toggles symlink resolution across `add`/`remove`/`query` (see table above) | `false` unless the value is exactly `"1"` |

## 7. Shell Integration Architecture

### Templating approach

Each supported shell has a dedicated Askama template file under `templates/` (`.txt` extension, `escape = "none"` — plain text, no HTML escaping): `bash.txt`, `elvish.txt`, `fish.txt`, `nushell.txt`, `posix.txt`, `powershell.txt`, `tcsh.txt`, `xonsh.txt`, `zsh.txt`. A `make_template!` macro in `src/shell.rs` derives one Rust wrapper struct per shell (`Bash<'a>(pub &'a Opts<'a>)`, etc.) implementing `askama::Template`, `Deref`ing to a shared `Opts` struct so templates can reference `{{ cmd }}`, `{{ hook }}`, `{{ echo }}`, `{{ resolve_symlinks }}` directly:

```rust
pub struct Opts<'a> {
    cmd: Option<&'a str>,       // z/zi command prefix, None if --no-cmd
    hook: InitHook,             // None | Prompt | Pwd
    echo: bool,                 // from _ZO_ECHO
    resolve_symlinks: bool,     // from _ZO_RESOLVE_SYMLINKS
}
```

`zoxide init <shell> [--no-cmd] [--cmd X] [--hook Y]` (`src/cmd/init.rs`) builds `Opts`, dispatches on `shell` (`InitShell`: `Bash/Elvish/Fish/Nushell/Posix(alias "ksh")/Powershell/Tcsh/Xonsh/Zsh`), calls `.render()`, and prints the result to stdout. There is no runtime templating in the deployed shell script itself — the output is static shell code, meant to be `eval`'d/sourced once per shell startup (top-level loader files `init.fish` and `zoxide.plugin.zsh` do `command -sq zoxide && zoxide init fish | source` / `eval "$(zoxide init zsh)"`).

### Hook modes and directory-change tracking

`InitHook` has three values, `--hook` default `pwd`:

| Mode | Semantics | bash | zsh | fish | posix |
|---|---|---|---|---|---|
| `None` | No `zoxide add` call is wired up at all | hook block entirely omitted | function defined but never registered into `precmd_functions`/`chpwd_functions` | omitted (`# -- not configured --`) | omitted |
| `Prompt` | Runs on every prompt draw, regardless of whether the directory changed | function installed into `PROMPT_COMMAND` | `precmd_functions+=(__zoxide_hook)` (native "before each prompt" hook) | `function __zoxide_hook --on-event fish_prompt` (native) | injected as a `$(...)` command substitution appended into `$PS1` itself — the **only** hook mode POSIX sh supports |
| `Pwd` | Runs only when the directory actually changes | **no native event** — implemented as Prompt + manual diff: caches `__zoxide_oldpwd`, compares against current pwd on every prompt draw, only calls `zoxide add` if they differ | `chpwd_functions+=(__zoxide_hook)` (native "on cd" hook) | `function __zoxide_hook --on-variable PWD` (native variable-watch) | **unsupported** — prints a diagnostic ("PWD hooks are not supported on POSIX shells... use --hook prompt instead") to **stdout** (no `>&2`) and does nothing further |

A `__zoxide_doctor` function (bash/zsh/posix only, not fish) runs once on first `z`/`zi` invocation to warn if the hook doesn't appear registered (e.g. sourced too early, or under VS Code's shell-integration wrapper), gated by an `_ZO_DOCTOR` flag so it only fires once per shell session.

### Known bug: Windows `cygpath` invocation is unwrapped in bash/zsh

`__zoxide_pwd`'s Windows branch is supposed to pipe the *output* of `pwd` through `cygpath -w` to convert a Cygwin/MSYS-style path to a native Windows path. In `bash.txt` and `zsh.txt`, this is rendered as `\command cygpath -w "{{ pwd }}"`, where `{{ pwd }}` is the **literal command text** (e.g. `\builtin pwd -L`), not wrapped in `$( )` — so the generated line literally passes the string `"\builtin pwd -L"` as an argument to `cygpath`, rather than the result of executing it. `posix.txt` and `fish.txt` do this correctly (`$(...)` / `(...)` substitution respectively). This is a real, currently-present regression traced to a specific commit that refactored the `pwd` command into a shared templated variable but dropped the substitution wrapper for bash/zsh only. It only affects Windows builds of zoxide running under bash or zsh (e.g. Git Bash/MSYS2); it is not exercised by the Linux-only `nix-dev` CI test suite, which is plausibly why it has gone uncaught. Relevant to a Go reimplementation: this bug should be a deliberate choice (replicate for exact behavioral parity, or fix) rather than something the port stumbles into unknowingly.

### Completions wiring per shell

- **bash** (requires Bash ≥4.4 for `${var@Q}`, interactive line-editing enabled, non-dumb terminal): registered via `complete -F __zoxide_z_complete -o filenames -- z`. For interactive (fuzzy/fzf) completion, since bash's completion callback can't synchronously replace-and-execute the command line, it stuffs a sentinel-prefixed placeholder into `COMPREPLY`, then binds the terminal's `\e[0n` Device-Status-Report reply to a helper function and fires `\e[5n` to trigger it asynchronously — the helper rewrites `READLINE_LINE`/`READLINE_POINT` and simulates Enter.
- **zsh**: same DSR-escape-sequence trick via a ZLE widget (`zle -N __zoxide_z_complete_helper`), registered into the completion system with `compdef __zoxide_z_complete z` (only if both `cmd` is set and `compdef` exists).
- **fish**: no escape-sequence trick needed — `commandline --replace` and `commandline --function repaint execute` can mutate and re-execute the command line synchronously. Registered via `complete --command __zoxide_z --no-files --arguments '(__zoxide_z_complete)'`.
- **posix**: no completion support at all (POSIX `sh`/`dash` has no completion framework).
- Non-interactive path (single existing directory, cursor not at end of line, or fewer than the minimum tokens) falls back to native filesystem directory completion in each shell rather than invoking zoxide.

Static completion **files** (as opposed to the interactive fuzzy-completion functions embedded in the init templates above) are generated at *build time* by `build.rs` into `contrib/completions/` (see §10) for bash, elvish, fig, fish, nushell, powershell, and zsh, and are what gets packaged/installed by package managers.

## 8. Interactive Selection (fzf integration)

zoxide does not implement its own fuzzy finder — `zoxide query -i` and `zoxide edit` both shell out to the external `fzf` binary via `std::process::Command`.

### Invocation

`Fzf::new()` resolves the binary (`which::which("fzf.exe")` on Windows to avoid an unsafe implicit CWD search; bare `"fzf"` via `$PATH` elsewhere) and sets base args applied to every invocation:
```
--delimiter=\t --nth=2 --read0
```
with both stdin and stdout piped.

### Input format

Every candidate is written to fzf's stdin as one NUL-terminated, tab-delimited record:
```
{score:>6.1}\t{path}\0
```
(6-char right-aligned score field, exactly 1 decimal place, clamped to `[0.0, 9999.0]`). `query -i` streams these one at a time as the DB `Stream` is walked; `edit` instead relies entirely on an `fzf` `start:reload(zoxide edit reload)` binding that re-execs `zoxide edit reload` to dump the whole (already sorted, `.rev()`'d) database.

### Output format

On exit, all of fzf's stdout is read into a `String`. `query -i` strips the leading 7 characters (6-char score + 1 separator) via `selection.get(7..)` unless `--score` was passed, in which case the full scored line is printed verbatim.

### Default options

`query -i` (`_ZO_FZF_OPTS` unset):
```
--exact --no-sort --bind=ctrl-z:ignore,btab:up,tab:down --cycle --keep-right
--border=sharp --height=45% --info=inline --layout=reverse --tabstop=1 --exit-0
```
plus an enabled preview pane (Unix only — no-op on Windows): `ls`-based directory listing (`--preview=... ls -Cp --color=always --group-directories-first {2..}` on Linux, without `--color`/`--group-directories-first` elsewhere), `--preview-window=down,30%,sharp`, `SHELL=sh` forced for the preview subprocess.

`edit` (fixed, independent of `_ZO_FZF_OPTS` — see override below):
```
--exact --no-sort
--bind=btab:up,ctrl-r:reload(zoxide edit reload),ctrl-d:reload(zoxide edit delete {2..}),
       ctrl-w:reload(zoxide edit increment {2..}),ctrl-s:reload(zoxide edit decrement {2..}),
       ctrl-z:ignore,double-click:ignore,enter:abort,start:reload(zoxide edit reload),tab:down
--cycle --keep-right --border=sharp --border-label="  zoxide-edit  "
--header=... --info=inline --layout=reverse --padding=1,0,0,0 --color=label:bold --tabstop=1
```
plus preview always enabled. `enter:abort` and `double-click:ignore` neutralize fzf's default "accept" bindings since the edit UI has nothing to `cd` into — it's a pure DB browser/editor; `Reload`/`Increment`/`Decrement`/`Delete` are hidden `zoxide edit <subcommand>` calls (`clap(hide = true)`) that mutate the DB and re-dump it.

### Override mechanism

`_ZO_FZF_OPTS` — read **only** by `query -i`'s `get_fzf()` (`edit` never checks it). If set, none of the built-in `--bind`/layout args above are passed; instead the raw value is assigned to the child process's `FZF_DEFAULT_OPTS` env var and fzf parses it itself. Setting `_ZO_FZF_OPTS` also implicitly **disables the preview pane**, since `.enable_preview()` is only called in the `else` (unset) branch.

### Exit-code handling

| fzf exit code | Result |
|---|---|
| 0 | `Ok(selection)` |
| 1 | error: "no match found" |
| 2 | error: "fzf returned an error" |
| 130 | silent process exit, code 130 (Ctrl-C/Esc) — via the general `SilentExit` mechanism described in §6 |
| 128–254 or signaled | error: "fzf was terminated" |
| other | error: "fzf returned an unknown error" |

## 9. Import Subsystem

`zoxide import <from> [--merge]` refuses to import into a non-empty database unless `--merge` is given. All six backends share one orchestration driver, `import::run()` (`src/import.rs`), which:

1. Loads `_ZO_EXCLUDE_DIRS` globs once.
2. Iterates the backend's `Iterator<Item = Result<Dir<'static>, ImportError>>`. For `Ok(dir)`: skips it if excluded, else calls `db.add_unchecked(path, rank, last_accessed)` (unconditional push, duplicates expected). For `Err(e)`: logs `"<path or line N>: <reason>"` to stderr and **continues** — a single malformed row never aborts the whole import.
3. After the loop, if the DB was actually modified: calls `db.dedup()` then `db.age(maxage())` — the same aging pass `zoxide add` uses.

Because every backend funnels through `add_unchecked` + `dedup()` + `age()`, imported ranks are **summed** (not decayed via the score-time-bucket formula) when duplicate paths merge, and the whole DB is subject to the 10,000-rank aging ceiling immediately after import.

### Supported sources

| Source | Location | Format | Per-record transform |
|---|---|---|---|
| `z` | `$_Z_DATA` or `~/.z` | One entry/line: `path\|rank\|last_accessed`, parsed via `rsplitn(3, '\|')` (from the right, so `\|` inside paths is preserved) | None |
| `fasd` | `$_FASD_DATA` or `~/.fasd` | Same `path\|rank\|last_accessed` format — reuses `z`'s parser directly | None |
| `z.lua` | `$_ZL_DATA` or `~/.zlua`; falls back to `$XDG_DATA_HOME/zlua/zlua.txt` or `~/.local/share/zlua/zlua.txt` if the primary file is missing | Same `path\|rank\|last_accessed` format — reuses `z`'s parser | None |
| `zsh-z` | `$ZSHZ_DATA`, else legacy `$_Z_DATA`, else `~/.z` | Same `path\|rank\|last_accessed` format — reuses `z`'s parser | None |
| `autojump` | OS-dependent: macOS `~/Library/autojump/autojump.txt`; Windows `$APPDATA/autojump/autojump.txt`; else `$XDG_DATA_HOME/autojump/autojump.txt` or `~/.local/share/autojump/autojump.txt` | One entry/line, **tab**-separated: `rank\tpath` | `rank = sigmoid(rank) = 1 / (1 + e^(-rank))` (autojump's raw scoring algorithm is too different to import verbatim); `last_accessed` hardcoded to `0` (no per-entry timestamp available) |
| `atuin` | Not a file — runs the subprocess `atuin history list --format={time}\t{directory} --print0` | NUL-separated records, each `{time}\t{directory}`, `{time}` formatted `YYYY-MM-DD HH:MM:SS` UTC, parsed via the `time` crate then converted to a Unix timestamp | `rank` hardcoded to `1.0` per visit; `last_accessed` = parsed timestamp; **consecutive** duplicate directories are collapsed within the iterator itself (non-consecutive repeats are left to the shared `dedup()` pass) |

## 10. Build, Test & Release Pipeline

### Build-time codegen (`build.rs`)

`build.rs` includes `src/cmd/cmd.rs` verbatim via `#[path = "src/cmd/cmd.rs"] mod cmd;` — so the build script parses the exact same `Cmd` clap definition the binary itself uses (single source of truth). It then generates shell completions for 7 shells (Bash, Elvish, Fig, Fish, Nushell, PowerShell, Zsh) via `clap_complete`/`clap_complete_fig`/`clap_complete_nushell`, writing them into a **hardcoded, in-source-tree** path, `contrib/completions/` (not Cargo's `OUT_DIR`) — these generated files (`zoxide.bash`, `zoxide.elv`, `zoxide.ts`, `zoxide.fish`, `zoxide.nu`, `_zoxide.ps1`, `_zoxide`) are committed to the repository. `cargo:rerun-if-changed` is scoped to `build.rs`/`src/`/`templates/`/`tests/` only (deliberately excluding `contrib/`) to avoid an infinite rebuild loop, since the generated output otherwise lives inside the watched package directory.

`.cargo/config.toml` also declares `[alias] xtask = "run --package xtask --"`, but no `xtask` package exists anywhere in the repository — this alias is currently vestigial/dead and can be disregarded for reimplementation purposes.

### Testing strategy

zoxide is binary-only (no `[lib]` target), so unit tests run via `cargo test --bin zoxide`, not `--lib`:
- Always-on unit tests: `Database::add`/`remove` round-trip tests (`src/db/mod.rs`), and a 14-case `rstest` suite for the keyword-matching filter (`src/db/stream.rs`) — 16 tests total with no feature flags.
- Feature-gated (`nix-dev`) tests, requiring 15 external interpreters/linters on `PATH` (provided by `shell.nix`): `src/shell.rs` renders every shell-init template across a 2(cmd)×3(hook)×2(echo)×2(resolve_symlinks) = 24-case matrix (via `rstest_reuse`) for each of 20 test functions = 480 tests, executing/formatting/linting the rendered output with real `bash`, `zsh`, `fish`+`fish_indent`, `dash`, `elvish`, `nu`, `pwsh`, `tcsh`, `xonsh`, `shellcheck`, `shfmt`, `black`, `mypy`, `pylint`. `tests/completions.rs` similarly runs the generated completion files (§ above) through `bash`, `fish`, `pwsh`, `zsh` (4 tests). Neither suite compiles at all without `--features nix-dev`.
- Canonical invocation: the `justfile`'s `test` recipe, `cargo nextest run --all-features --no-fail-fast --workspace`, run inside `nix-shell --pure`.

### CI/CD

The repo has **five** GitHub Actions workflow files: `ci.yml`, `release.yml`, `cd.yml`, `winget.yml`, and `no-response.yml`.

- **`ci.yml`**: on push/PR to `main`, runs `just lint test` on `ubuntu-latest` inside a Nix environment (via `cachix/install-nix-action` + Cachix caching). `just lint` runs `cargo fmt --check`, `cargo clippy --all-features --all-targets -- -Dwarnings`, `cargo msrv verify`, `cargo udeps`, `mandoc -Tlint` (man pages), `markdownlint`, `nixfmt --check`, `shellcheck`/`shfmt` (against `install.sh`), `yamlfmt -lint`. **Known wrinkle:** the job's `matrix` currently defines only `os: [ubuntu-latest]`, yet several steps are still conditioned on `matrix.os == 'windows-latest'` (installing a stable+`clippy` toolchain and a nightly+`rustfmt` toolchain via `actions-rs/toolchain@v1`) — these steps are dead/unreachable in the current configuration, a leftover from what was presumably once a multi-OS matrix.
- **`release.yml`**: builds 12 cross-compiled targets — 6 Linux musl architectures (via `cross`: `x86_64-unknown-linux-musl`, `arm-unknown-linux-musleabihf`, `armv7-unknown-linux-musleabihf`, `aarch64-unknown-linux-musl`, `i686-unknown-linux-musl`, `riscv64gc-unknown-linux-musl`; **5 of these 6** — all except `arm-unknown-linux-musleabihf` — also produce a `.deb` via `cargo-deb`), 2 Android targets, 2 native macOS targets, 2 native Windows MSVC targets (statically-linked CRT via `.cargo/config.toml` rustflags, see §1). Packages each target's binary + `CHANGELOG.md`/`LICENSE`/`README.md`/`man/`/`contrib/completions/` into a `.tar.gz` (Unix) or `.zip` (Windows), uploads as CI artifacts, and creates a **draft** GitHub release gated on the commit message on `main` starting with `chore(release)`.
- **`cd.yml`**: on `v*.*.*` tag push, publishes to crates.io via OIDC auth (`cargo publish`).
- **`winget.yml`**: on GitHub release publication, publishes the Windows MSVC zip artifact to the Windows Package Manager repo.
- **`no-response.yml`**: triggers on new issue comments and a daily cron; auto-closes issues labeled `waiting-for-response` after 30 days of no reply (via `lee-dohm/no-response`), guarded to only run on the upstream repo.
- **Release profile** (`Cargo.toml`): `codegen-units = 1`, `debug = 0`, `lto = true`, `strip = true`.

### Packaging

Debian packaging (`cargo-deb`, driven by `[package.metadata.deb]` in `Cargo.toml`) installs the binary to `/usr/bin/`, bash/fish/zsh completions (only 3 of the 7 generated completion files) to their respective vendor directories, man pages to `/usr/share/man/man1/`, and docs to `/usr/share/doc/zoxide/`. 6 man pages exist (`zoxide.1`, `zoxide-{add,import,init,query,remove}.1`) — notably no `zoxide-edit.1` despite `edit` being a real top-level subcommand.

## 11. Design Philosophy

zoxide's own README states its purpose directly (no separate "goals/non-goals" section exists in the project):

> "zoxide is a smarter cd command, inspired by z and autojump. It remembers which directories you use most frequently, so you can 'jump' to them in just a few keystrokes. zoxide works on all major shells."

Key implications drawn directly from the README's framing:

- **It positions itself as a superset of `cd`, not a replacement paradigm.** The "Getting started" usage examples explicitly demonstrate that ordinary `cd`-style invocations still work through `z`: `z ~/foo` ("z also works like a regular cd command"), `z foo/` ("cd into relative path"), `z ..` ("cd one level up"), `z -` ("cd into previous directory") — alongside its differentiating fuzzy/ranked behavior (`z foo`, `z foo bar`, `z foo /`).
- **Cross-shell support is the one explicit differentiator** called out in the pitch itself ("works on all major shells") — bash, zsh, fish, posix/ksh, powershell, tcsh, xonsh, elvish, nushell are all first-class, each with its own hand-tuned template rather than one generic script.
- **Interactive selection is explicitly optional and externally sourced**, not a core dependency: fzf is documented as a separate, optional install ("Install fzf (optional)"), with a stated minimum supported version (v0.51.0), and its absence only disables `-i`/`zi`/interactive tab-completion, not core `z` functionality.
- **No stated non-goals.** The README does not discuss performance/speed claims, does not position itself against `z`/`autojump` beyond "inspired by," and defers the frecency algorithm's exact mechanics to an external wiki page rather than documenting it in the README body (the algorithm is, however, fully present and readable in source — see §4 above, which was independently verified against `src/db/dir.rs` and `src/db/mod.rs`).

## 12. Appendix: Considerations for a Go Reimplementation

**These are open options for the `zjump` team to evaluate, not decisions.** Each row lists the Rust technique/dependency from zoxide and plausible idiomatic Go directions worth prototyping and comparing — none are prescribed here.

| Rust technique / crate | Purpose in zoxide | Go directions to evaluate |
|---|---|---|
| `clap` (derive macros) | Declarative CLI/subcommand parsing, help generation, shell-completion generation | `spf13/cobra` (widely used, has completion generation), `alecthomas/kong` (struct-tag driven, closer to clap's derive style), `urfave/cli`, or stdlib `flag` + hand-rolled subcommand dispatch. Worth weighing "declarative struct tags" (kong) vs. "imperative command tree" (cobra) against how closely the CLI surface needs to mirror clap's generated help/completions. |
| `bincode` + `serde` (binary DB format) | Fixed-layout, fast binary (de)serialization of `Vec<Dir>` | Options range from a hand-written binary reader/writer using stdlib `encoding/binary` (mirrors the exact byte layout documented in §3, enabling direct `db.zo` file compatibility/migration), to `encoding/gob` (idiomatic but Go-specific, not compatible with zoxide's format), to a general encoder like `vmihailenco/msgpack` or protobuf. A key open question: does `zjump` need read-compatibility with existing `db.zo` files (for migration from zoxide), or is a clean-slate Go-native format acceptable? |
| `askama` (compile-time templates) | Renders 9 shell-init scripts from a shared `Opts` struct | Go has no direct compile-time-checked template equivalent; closest idiomatic options are stdlib `text/template` (runtime-parsed, would need `go:embed` to bundle `templates/*.txt`-equivalents into the binary) or a `go generate`-based code-gen step that pre-renders/validates templates at build time to approximate Askama's compile-time safety. If the port targets Windows bash/zsh support, decide deliberately whether to replicate or fix the `cygpath` substitution bug noted in §7. |
| `dirs` / `dunce` (directory resolution) | XDG/platform data-dir lookup; Windows canonicalization without UNC prefixes | `os.UserHomeDir()`/`os.UserCacheDir()` (stdlib) cover basic cases; `adrg/xdg` more precisely mirrors `dirs::data_local_dir()`'s XDG-spec behavior on Linux. No direct stdlib equivalent to `dunce`'s UNC-prefix stripping — `filepath.EvalSymlinks` behaves differently; this would need a small custom helper if Windows UNC-prefix avoidance is required. Also worth preserving the two-tier `resolve_path` (lexical) vs. `canonicalize` (filesystem+symlinks) split described in §6, since `add`/`remove`/`query` each rely on that distinction differently. |
| `glob` (pattern matching for excludes/queries) | `_ZO_EXCLUDE_DIRS` glob parsing, `Pattern::escape` for literal-path defaults | stdlib `path/filepath.Match` is limited (no `**`); `gobwas/glob` or `bmatcuk/doublestar` offer richer semantics closer to the `glob` crate. Worth checking each candidate's exact escaping/matching semantics against zoxide's documented default-exclude behavior (home directory matched literally, not recursively). |
| `Fzf`/`FzfChild` (subprocess integration) | Spawns external `fzf`, pipes NUL-delimited tab-separated records to stdin, reads selection from stdout, maps exit codes | This is a straightforward, low-risk port: stdlib `os/exec.Command` with `StdinPipe()`/`StdoutPipe()` covers the same protocol directly, no third-party library needed. The exit-code-to-behavior mapping (0/1/2/130/128-254) and NUL-delimited `--read0` record format are protocol-level, not Rust-specific. |
| `ouroboros` (self-referential zero-copy `Database`) | Lets `Dir` path strings borrow from the DB's own loaded byte buffer without copying | Not generally needed in Go: Go's GC and immutable strings make zero-copy self-reference far less load-bearing than in a borrow-checked language. A plain struct holding an owned `[]byte` plus a `[]Dir` with regular (copied) `string` path fields is likely simplest; `unsafe.String` over sub-slices could reclaim the zero-copy behavior later if profiling shows it matters. |
| `anyhow` (error handling/context chains) | `Context`-style error wrapping, printed as a full causal chain on failure; a `SilentExit`-style sentinel type for quiet non-zero exits (broken pipes, fzf Ctrl-C) | Idiomatic Go equivalents: stdlib `errors.New`/`fmt.Errorf("...: %w", err)` wrapping, optionally paired with a small `errors.Join`/multi-line formatter to reproduce anyhow's `{:?}` full-chain printing behavior, plus a small sentinel error type (checked via `errors.As`) to reproduce the "exit with code N, print nothing" pattern described in §6. |
| `fastrand` + custom tmpfile/rename dance (atomic writes) | Random temp-file naming in the target directory, write+fsync+chown+rename, cleanup on failure | Go's stdlib `os.CreateTemp` (with `dir` set to the target's parent) plus `os.Rename` already covers most of this dance; `Sync()` + `os.Chown` (Unix) can be layered on to match zoxide's fsync-before-rename and best-effort ownership-preservation behavior. |
| `time` crate (atuin timestamp parsing) | Parses atuin's fixed `YYYY-MM-DD HH:MM:SS` UTC format | Trivial in Go via stdlib `time.Parse` with a matching reference-time layout string; no third-party dependency needed. |
| `rstest`/`rstest_reuse` (parameterized/cross-product test cases) | Table-driven tests, 24-case option matrix reused across 20 shell-template test functions | Go's idiomatic table-driven test pattern (`[]struct{...}` + `t.Run` subtests, possibly with nested loops for the cross-product) covers this without needing an external library. |
