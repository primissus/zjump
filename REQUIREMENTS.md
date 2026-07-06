# Requirements

The scope contract for the initial `zjump` build. It defines **what zjump will
actually achieve** in this effort. PLAN.md defines **how**. Both are grounded in
the parity surface documented in [`DESIGN.md`](./DESIGN.md) and
[`ARCHITECTURE.md`](./ARCHITECTURE.md); those remain the authoritative behavioral
spec, and any deliberate deviation from them is called out explicitly below.

Requirements carry stable IDs (`R-<AREA>-<n>`) so PLAN.md, tests, and the
DESIGN.md §12 parity table can reference them.

---

## 1. Scope at a glance

zjump targets **Broad parity (Tier 2 of the scoping discussion), minus fish and
minus the `import` subcommand**. In one sentence: a working, frecency-ranked
`cd` replacement with five subcommands, zsh + bash integration, and fzf-driven
interactive selection.

Decisions locked with the user:

| Decision | Choice |
|---|---|
| Overall tier | Broad parity (add/query/remove/init/edit + fzf), minus fish, minus import |
| Matching algorithm | zoxide-exact right-to-left substring matcher — **no** edit-distance |
| Shells for `init` | zsh and bash only |
| fzf interactive (`query -i`, `zi`, `edit` UI) | In scope |
| Interactive tab-completion | bash + zsh only |
| `import` subcommand | Out of scope |
| DB format | zjump-native — **not** byte-compatible with zoxide's `db.zo` |
| Target platforms | Unix (Linux, macOS). Windows out of scope |

---

## 2. In-scope requirements

### 2.1 `add` — track a directory

- **R-ADD-1** `zjump add <paths>...` adds or increments each path using
  `add_update` semantics: on an existing path `rank += by` **and**
  `last_accessed = now`; on a new path, insert with `rank = by`,
  `last_accessed = now`. Rank is floored at `0.0`, never negative.
- **R-ADD-2** `-s/--score <f64>` sets the increment `by` (default `1.0`).
- **R-ADD-3** Silently skip (no error) any path containing `\n` or `\r`.
- **R-ADD-4** Silently skip any path matching a `_ZJUMP_EXCLUDE_DIRS` glob.
- **R-ADD-5** Error `"not a directory: {path}"` when the resolved path is not a
  directory.
- **R-ADD-6** Resolve each path by full canonicalization (symlinks followed)
  when `_ZJUMP_RESOLVE_SYMLINKS=1`, else by purely lexical resolution.
- **R-ADD-7** Load exclude/maxage config **before** opening the DB, so a
  malformed env var fails fast even if all paths would be skipped.
- **R-ADD-8** After any change, run the aging pass (R-DB-4) using
  `_ZJUMP_MAXAGE`, then save atomically.
- **R-ADD-9** Error on an invalid system clock (time before the Unix epoch).

### 2.2 `query` — search and pick

- **R-QRY-1** `zjump query [keywords]...` (default mode) prints the single
  best-scoring match; errors `"no match found"` when nothing matches.
- **R-QRY-2** When the top match equals `--exclude`, keep pulling the next
  match; error `"you are already in the only match"` if none remain.
- **R-QRY-3** `-l/--list` prints every matching path (one per line), skipping
  the `--exclude`d path.
- **R-QRY-4** `-i/--interactive` funnels matches through fzf (see §2.6).
  Conflicts with `--list`.
- **R-QRY-5** `-s/--score` prefixes each result with its decayed frecency score.
- **R-QRY-6** `-a/--all` includes currently-nonexistent directories and disables
  both the existence filter and the existence-based lazy deletion (R-QRY-10b).
- **R-QRY-7** `--exclude <path>` skips a path by **exact string equality**
  (never resolved); this mechanism **never deletes** anything from the DB.
- **R-QRY-8** `--base-dir <path>` restricts results to entries component-wise
  under that path; the value is used verbatim (never resolved).
- **R-QRY-9** Keyword matching (R-MATCH-*) and frecency ordering (R-MATCH-4)
  govern which entries match and in what order (best first).
- **R-QRY-10** Lazy deletions during `query` (default/`--list`/`--interactive`):
  **(a)** any candidate matching a `_ZJUMP_EXCLUDE_DIRS` glob is permanently
  removed from the DB the moment it is evaluated (distinct from `--exclude`,
  which never deletes — R-QRY-7); **(b)** a candidate whose directory no longer
  exists is removed **only if it is also stale** (`last_accessed < now − 90 days`,
  i.e. `now − 3×MONTH` with `MONTH = 2,592,000 s`) — a missing-but-recently-used
  directory (e.g. an unmounted drive) is hidden from results but **retained** in
  the DB. `-a/--all` disables (b) entirely.

### 2.3 `remove` — delete by hand

- **R-RM-1** `zjump remove [paths]...` removes each path; a no-op if empty.
- **R-RM-2** For each path: try an exact string match; on failure, retry against
  the lexically-resolved absolute path (no symlink resolution).
- **R-RM-3** Error `"path not found in database: {path}"` when neither the exact
  nor the resolved lookup succeeds (or when resolving is a no-op and the first
  lookup already failed).

### 2.4 `init` — shell integration (zsh, bash)

- **R-INIT-1** `zjump init <shell>` emits the integration script for `zsh` and
  `bash`; any other shell value is rejected.
- **R-INIT-2** `--no-cmd` (alias `--no-aliases`) suppresses the jump commands,
  emitting only the tracking hook and completions.
- **R-INIT-3** `--cmd <cmd>` renames the jump command (default `z`); the
  interactive variant is always `<cmd>i`.
- **R-INIT-4** `--hook none|prompt|pwd` (default `pwd`) selects the tracking
  hook; `pwd` on bash is emulated as prompt + cached-oldpwd diffing.
- **R-INIT-5** The `z` function reproduces zoxide's argument dispatch: no args →
  home; `-` → `$OLDPWD`; a single existing directory → `cd` directly; `-- path`
  → `cd` directly; otherwise `query --exclude "$PWD" -- "$@"`.
- **R-INIT-6** `zi` runs `query -i` regardless of argument count.
- **R-INIT-7** A `pwd` helper reports the directory with `-L`/`-P` chosen by
  `_ZJUMP_RESOLVE_SYMLINKS`.
- **R-INIT-8** `_ZJUMP_ECHO=1` makes the generated `cd` wrapper print the matched
  directory before navigating.
- **R-INIT-9** Emit interactive Space-Tab tab-completion for bash and zsh
  (see §2.6). Bash completion requires bash ≥ 4.4 (for `${var@Q}` quoting) and an
  interactive, non-dumb terminal; it degrades to native directory completion
  otherwise.
- **R-INIT-10** *(optional / nice-to-have)* A `doctor` check that warns once per
  session if the hook does not appear registered.

### 2.5 `edit` — interactive database editor

- **R-EDIT-1** `zjump edit` (no subcommand) launches an fzf-driven browser over
  the database, sorted best-score-first.
- **R-EDIT-2** Hidden subcommands `increment`/`decrement`/`delete`/`reload`
  back the fzf key bindings: `+1.0`, `-1.0` (floored at 0), remove, and a no-op
  re-dump respectively. On an **already-present** entry (the normal case),
  `increment`/`decrement` change only `rank` and leave `last_accessed`
  **untouched** — the `add` mutator, in contrast to `add_update` — so editing
  never shifts an entry into a different recency bucket. Each prints the entry
  list as `score\tpath\0` records.
- **R-EDIT-3** Key bindings match zoxide's edit UI (ctrl-r/ctrl-d/ctrl-w/ctrl-s,
  tab/btab, `enter:abort`, `ctrl-z:ignore`, `double-click:ignore`), with the
  preview pane always enabled.
- **R-EDIT-4** `increment`/`decrement` on a missing path fall back to inserting a
  new entry with `last_accessed = now` (matching `add`).

### 2.6 fzf interactive selection

- **R-FZF-1** `query -i` and `edit` shell out to the external `fzf` binary; if
  absent, fail with `"could not find fzf, is it installed?"`.
- **R-FZF-2** Candidates are streamed as NUL-terminated, tab-delimited
  `"{score:>6.1}\t{path}\0"` records; fzf runs with
  `--delimiter=\t --nth=2 --read0` so it searches the path field only.
- **R-FZF-3** `query -i` uses zoxide's built-in fzf arg set plus an `ls`-based
  preview pane, and on selection strips the 7-char score prefix unless `--score`
  was passed.
- **R-FZF-4** `_ZJUMP_FZF_OPTS`, when set, replaces the built-in args (passed via
  `FZF_DEFAULT_OPTS`) and disables the preview pane. It is read by `query -i`
  only, never by `edit`.
- **R-FZF-5** Map fzf exit codes: `0` → selection, `1` → `"no match found"`,
  `2` → `"fzf returned an error"`, `130` → **silent** exit 130,
  `128`–`254`/signaled → `"fzf was terminated"`, else
  `"fzf returned an unknown error"`.
- **R-FZF-6** Require fzf ≥ v0.51.0 (documented; matches upstream).

### 2.7 Matching & scoring

- **R-MATCH-1** Keyword matching is case-insensitive, plain-substring (not
  fuzzy/edit-distance), evaluated right-to-left.
- **R-MATCH-2** The last keyword's match must lie within the final path
  component (no path separator after it).
- **R-MATCH-3** Each earlier keyword must appear, in order, strictly to the left
  of the previously matched span (no overlap).
- **R-MATCH-4** The decayed frecency score is `rank ×` {`4` if `<1h`, `2` if
  `<1d`, `0.5` if `<1w`, else `0.25`}, using `now − last_accessed` with
  saturating subtraction. Sorting is ascending by score; results are yielded
  best-first. `--score` display is clamped to `[0.0, 9999.0]`, formatted
  `{score:>6.1}`.

### 2.8 Database & persistence

- **R-DB-1** A single on-disk file in a zjump-native, versioned format (see
  PLAN.md §4). It is **not** byte-compatible with zoxide's `db.zo`, and lives
  under a zjump-specific data directory and filename so it can never be confused
  with or overwrite a real zoxide database.
- **R-DB-2** All writes are atomic: write a randomly-named temp file in the same
  directory, `Sync`, best-effort preserve owner uid/gid on Unix, then `rename`
  over the target; clean up the temp file on any failure.
- **R-DB-3** The data directory is created eagerly; the DB file is not written
  until the first save with actual changes.
- **R-DB-4** Aging pass: if the sum of all raw ranks exceeds `_ZJUMP_MAXAGE`,
  scale every rank by `0.9 × max_age / total`, then delete entries whose
  post-scaling rank falls below `1.0`.
- **R-DB-5** A dedup helper merges same-path entries (sum ranks, max
  `last_accessed`). *(Not reachable in this scope: `add`/`add_update` — including
  `edit`'s insert-if-missing path — proactively avoid duplicates via a linear
  existing-path check, so this adjacent-merge helper is exercised only by the
  `import` subcommand. Retained purely for future work, F-3.)*
- **R-DB-6** Loads are size-guarded (fail fast on an implausibly large/corrupt
  file) and reject an unknown format version.

### 2.9 Environment variables

Six variables, `_ZJUMP_*`-prefixed, mirroring zoxide's `_ZO_*` semantics
one-for-one:

- **R-ENV-1** `_ZJUMP_DATA_DIR` — data directory for the DB file; must be an
  absolute path (validated unconditionally), else error.
- **R-ENV-2** `_ZJUMP_ECHO` — echo the matched directory before navigating;
  true only when the value is exactly `"1"`.
- **R-ENV-3** `_ZJUMP_EXCLUDE_DIRS` — OS-path-list of glob patterns excluded from
  `add` and lazily purged during `query`; if unset, defaults to a single pattern
  matching the home directory **literally** (glob-escaped, not recursive).
- **R-ENV-4** `_ZJUMP_FZF_OPTS` — custom fzf flags; read by `query -i` only.
- **R-ENV-5** `_ZJUMP_MAXAGE` — aging ceiling; parsed as `u32` then treated as
  float; default `10000`.
- **R-ENV-6** `_ZJUMP_RESOLVE_SYMLINKS` — resolve symlinks in `add`/`query`;
  true only when the value is exactly `"1"`.

### 2.10 Process & error semantics

- **R-ERR-1** A broken-pipe I/O error on **any** stdout (or fzf-stdin) write, in
  every output-producing subcommand, results in a **silent exit 0** (e.g.
  `zjump query --list | head`). Any other I/O error is reported with a
  `"could not write to {device}"`-style context.
- **R-ERR-2** Interactive-picker cancellation (fzf exit 130) results in a
  **silent exit 130**.
- **R-ERR-3** Genuine errors print as `zjump: <full causal chain>` to stderr —
  the entire chain of wrapped context, not a bare message — and never a Go stack
  trace/panic dump.
- **R-ERR-4** The invalid-system-clock error (system time before the Unix epoch)
  applies to **every** subcommand that reads the current time — `add` (R-ADD-9),
  `query`, and `edit`. `remove` never reads the clock and therefore has no
  clock-error case.

---

## 3. Out of scope (explicit non-goals for this effort)

- **N-1** The `import` subcommand and all six source backends (atuin, autojump,
  fasd, z, z.lua, zsh-z).
- **N-2** `init` for fish, posix/ksh, powershell, tcsh, xonsh, elvish, nushell.
- **N-3** Byte-compatible read/write of zoxide's `db.zo` binary format.
- **N-4** Windows / cross-platform support: PowerShell hooks, `cygpath`, UNC
  path stripping, `which fzf.exe`. Unix (Linux/macOS) only.
- **N-5** Build-time completion-file generation, man pages, `.deb`/OS packaging.
- **N-6** Levenshtein / edit-distance matching (the one capability inherited from
  Fzfoxide is deliberately dropped in favor of the parity substring matcher).

---

## 4. Deliberate deviations from zoxide

Because Tier 2 is broad parity, not bug-for-bug parity, these behaviors
intentionally differ from upstream and must be documented in the code and
README:

- **D-1** zjump-native DB format and data dir/filename (vs. zoxide's `db.zo`).
- **D-2** `_ZJUMP_*` env-var prefix (vs. `_ZO_*`).
- **D-3** `edit` re-sorts after every mutating reload, fixing zoxide's
  session-scoped sort-staleness quirk (DESIGN.md §2.5).
- **D-4** `query` rewrites the DB **only when actually dirty**, rather than
  unconditionally on every invocation (DESIGN.md §2.2 / ARCHITECTURE.md §3).
  Lazy deletions and aging still persist as usual.
- **D-5** No Windows `cygpath` behavior at all (the upstream bug in
  ARCHITECTURE.md §7 is therefore moot here).

---

## 5. Future considerations (recorded, not committed)

- **F-1** A `db.zo → zjump` migration script (user-requested), converting an
  existing zoxide database into zjump's native format.
- **F-2** fish and the remaining shells for `init`.
- **F-3** The `import` subcommand.
- **F-4** Windows / full cross-platform support.
- **F-5** Optionally honoring `_ZO_*` env vars as a compatibility fallback.

---

## 6. Acceptance criteria

The effort is "done" when:

- **A-1** A user can `eval "$(zjump init zsh)"` / `... bash`, navigate normally,
  and `z <keywords>` lands in the correct highest-frecency directory.
- **A-2** The keyword matcher passes a table-driven parity suite mirroring
  zoxide's cases (case-folding, final-component anchoring, overlap rejection).
- **A-3** Frecency ordering matches zoxide's for a fixed fixture database across
  all four recency buckets.
- **A-4** `add`/`query`/`remove` round-trip correctly through save-then-reload;
  the aging pass rescales/prunes as specified.
- **A-5** `query -i` and `zi` invoke fzf and return the selection; Ctrl-C exits
  silently with code 130.
- **A-6** `edit` mutates the DB via its key bindings (increment/decrement/delete)
  and re-dumps the list.
- **A-7** `zjump query --list | head` exits 0 with no error output (broken-pipe
  tolerance).
- **A-8** `go test ./...` is green with no external dependencies; tests that
  shell out to real `bash`/`zsh`/`fzf` are gated behind an explicit build tag.
