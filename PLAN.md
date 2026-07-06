# Implementation Plan

How `zjump` gets built. Scope is fixed by [`REQUIREMENTS.md`](./REQUIREMENTS.md);
behavior is governed by [`DESIGN.md`](./DESIGN.md) and
[`ARCHITECTURE.md`](./ARCHITECTURE.md). This plan also records the review of the
`Fzfoxide` Go prototype (`~/src/Fzfoxide`) and exactly what we extract from it.

> **Note on process:** this plan was signed off and the build is complete — per
> AGENTS.md, current status is **implemented** for the committed scope. This
> section is kept for historical record of how the build was planned; see
> AGENTS.md/README.md for current status.

---

## 1. Scope recap & open decisions

**Committed scope (see REQUIREMENTS.md §1):** Broad parity — `add`, `query`
(incl. `-i`), `remove`, `init` (zsh + bash), `edit`, with fzf interactive
selection and bash/zsh tab-completion. No `import`, no fish, no Windows,
zjump-native DB, zoxide-exact substring matcher.

**Open decisions, as resolved during the build** (each had a recommended
default; all are now settled since the build is complete). O-2 and O-6 were
already **locked** as deviations D-2/D-4 (REQUIREMENTS.md §4) and are listed
only for traceability:

| # | Decision | Recommended default | Alternatives |
|---|---|---|---|
| O-1 | Concrete DB encoding | Versioned length-prefixed **binary** via `encoding/binary` (stdlib), mirroring zoxide's layout in spirit → makes the future db.zo migration script trivial | JSON (human-readable, max reuse of Fzfoxide's approach); `encoding/gob` |
| O-2 | Env-var prefix | **Locked:** `_ZJUMP_*`, six vars mirroring `_ZO_*` (deviation D-2) | *(future F-5: optional `_ZO_*` fallback)* |
| O-3 | Data dir / filename | `${XDG_DATA_HOME:-~/.local/share}/zjump/db` | `~/.zjump` (flat, like Fzfoxide's `~/.fzfoxide`) |
| O-4 | Module path | ~~`github.com/<owner>/zjump`~~ **Resolved: local module `zjump`** (see `go.mod`) | `github.com/<owner>/zjump` |
| O-5 | CLI parsing | stdlib `flag` + a small hand-rolled subcommand dispatcher (zero deps, satisfies AGENTS.md "prefer stdlib") | `alecthomas/kong` (declarative, closest to clap); `spf13/cobra` |
| O-6 | Query write behavior | **Locked:** conditional dirty-only write (deviation D-4) | — |

---

## 2. Review of the Fzfoxide prototype — what we extract

Fzfoxide (`~/src/Fzfoxide`, ~492 LOC) is an early personal prototype, not a
parity tool. Its CLI exposes `--run`/`--record`/`--query` but only `--run` is
wired up. It stores `Directory{Path, Count int, LastAccessed time.Time}` as
indented JSON at `~/.fzfoxide` (non-atomic `os.WriteFile`), and matches with a
**Levenshtein edit-distance** similarity per path component (≥50%), tie-broken
by raw `Count`. It has no frecency decay, aging, dedup, exclude globs, symlink
handling, atomic writes, `remove`/`edit`/`init`/`import`, env vars, or fzf.

**Honest verdict: ~5–10% of the work is accelerated, mostly as _shape_ rather
than reusable code.** Extraction matrix:

| Fzfoxide asset | Verdict | How it maps into zjump |
|---|---|---|
| `cmd/fzfoxide/main.go` dispatch (`--run` etc.) | **Reuse shape only** | Replace with a proper subcommand tree (`add`/`query`/`remove`/`init`/`edit`); the `--run`/`--record`/`--query` design is discarded for parity. |
| `Directory{Path, Count, LastAccessed}` struct | **Adapt** | Becomes `Dir{Path string, Rank float64, LastAccessed uint64}` — `Count int`→`Rank float64`, `time.Time`→epoch seconds. |
| `readDatabase`/`writeDatabase` (JSON @ `~/.fzfoxide`) | **Adapt as seed** | Keep the load/marshal shape; add a version header, atomic temp+rename, size guard, and XDG data-dir resolution. Format per O-1. |
| `RecordDirectory` (linear scan, `Count++`) | **Adapt** | Becomes `add_update` (rank += by, `last_accessed = now`, rank floored at 0) + trigger the aging pass. |
| `Run` fast-path (cd into an existing dir) | **Reuse logic, relocate** | Move the "single existing dir → cd directly" behavior into the generated `z` shell function (parity), not the binary. |
| `getDatabasePath` var-for-DI test pattern | **Reuse** | Good testability idiom; keep an injectable data-dir/clock seam for unit tests. |
| `pkg/fuzzy` Levenshtein + tests | **Discard** | Deviates from the parity substring matcher (N-6). Delete; do not carry the dependency forward. |
| `scripts/setup.sh` `cd`-override | **Discard mechanism, keep as inspiration** | Replaced by real `init` templates + hooks; setup.sh's zsh `cd()` wrapper is a primitive precursor to the `z` function. |
| `scripts/pushbin.sh`, `.github/workflows/go.yml` | **Reuse/adapt** | Build + CI scaffolding; extend CI to run the gated shell/fzf tests. |
| Test files (DI, table-driven) | **Reuse pattern** | Mirror the injectable-DB and table-driven style for the new engine. |

**Net:** we salvage the project skeleton, the entry-struct + persistence seed,
the record + fast-path-cd behavior, and the testing idioms. The scoring, matcher,
aging, dedup, atomic writes, all five subcommands, shell templates, fzf plumbing,
and env-var config are built fresh against the specs.

---

## 3. Target architecture

Standard Go layout (per AGENTS.md), stdlib-first:

```
zjump/
  cmd/zjump/main.go        # thin entrypoint: strip backtraces, parse, dispatch, map SilentExit
  internal/cli/            # subcommand definitions, flag parsing, dispatch, help footer
  internal/db/
    dir.go                 # Dir struct, score(), display formatting
    db.go                  # Database: load/save (atomic), add/add_update/remove, age, dedup, sort
    format.go              # versioned encode/decode (O-1)
    stream.go              # candidate stream + filter pipeline (keywords, base-dir, exclude, exists)
    match.go               # right-to-left substring matcher
  internal/config/         # env-var reading + validation (_ZJUMP_*), data-dir resolution
  internal/shell/
    shell.go               # Opts, template dispatch, embed
    templates/zsh.tmpl     # go:embed
    templates/bash.tmpl
  internal/fzf/            # fzf subprocess: args, NUL/tab protocol, exit-code mapping
  internal/errs/           # SilentExit sentinel, broken-pipe handling, causal-chain printer
  internal/paths/          # resolve_path (lexical) vs canonicalize (fs+symlink) helpers
```

**Data model** (no `ouroboros` equivalent needed — Go strings + GC make
zero-copy self-reference unnecessary; ARCHITECTURE.md §12):

```go
type Dir struct {
    Path         string
    Rank         float64
    LastAccessed uint64 // Unix epoch seconds
}

type Database struct {
    path  string
    dirs  []Dir
    dirty bool
}
```

**Key algorithms — implement exactly per spec:**

- **Frecency score** (ARCHITECTURE.md §4): buckets `<3600→×4`, `<86400→×2`,
  `<604800→×0.5`, `else ×0.25`; `saturating_sub` for clock skew; sort ascending
  by score with a total-order float compare, iterate reverse.
- **Aging** (ARCHITECTURE.md §4): `total = Σ rank`; if `total > max_age`,
  `factor = 0.9·max_age/total`, scale all, drop `< 1.0`.
- **Dedup** (ARCHITECTURE.md §4): sort by path, merge adjacent equals (sum rank,
  max last_accessed).
- **Matcher** (ARCHITECTURE.md §5): lowercase; `rfind` last keyword, reject if a
  path separator follows it; truncate left; each earlier keyword `rfind` to the
  left, in order, no overlap.
- **Lazy-deletion TTL** (ARCHITECTURE.md §5): `MONTH = 2_592_000 s`;
  `ttl = now − 3×MONTH` (90 days). During `query` (unless `--all`), a
  nonexistent entry is removed only when `last_accessed < ttl`, else hidden from
  results but retained; the stream options carry `ttl`.
- **Atomic write** (ARCHITECTURE.md §3): `os.CreateTemp` in the target dir,
  `Write`, `Sync`, best-effort `Chown` to the original owner, `os.Rename`,
  cleanup on failure.
- **SilentExit** (ARCHITECTURE.md §6): sentinel error checked via `errors.As`;
  broken-pipe → code 0 (silent), fzf 130 → code 130 (silent); all other errors →
  `zjump: <chain>` via `%w`-wrapped `fmt.Errorf` printed with full context.

---

## 4. Database format (recommended, O-1)

A zjump-native, versioned binary layout — stdlib `encoding/binary`, little-endian
— chosen so the future db.zo migration script (F-1) is a near-trivial
format-to-format transform:

```
offset 0 : u32  magic/version  (e.g. version = 1; reject unknown)
offset 4 : u64  entry count
per entry:
   u64  path length
   ...  path bytes (UTF-8)
   f64  rank
   u64  last_accessed (epoch seconds)
```

- Reads are bounded by a size guard (reject files larger than a sane cap, e.g.
  32 MiB, like zoxide) and by a version check.
- Distinct filename/dir (O-3) guarantees it can never be mistaken for `db.zo`.
- If O-1 lands on JSON instead: wrap entries in `{ "version": 1, "entries": [...] }`
  and keep the same atomic-write + version/size guards. (JSON maximizes reuse of
  Fzfoxide's code but is larger and slower; the DB is tiny in practice, so either
  is acceptable — binary is recommended for the migration-script symmetry.)

---

## 5. Phased roadmap

Each phase has a concrete deliverable and an exit criterion. Phases are ordered
so the engine is proven before UI/shell glue piles on. Tests are written
alongside each phase (AGENTS.md), not deferred.

### Phase 0 — Scaffolding & extraction
- Create `go.mod` (O-4), the package layout (§3), CI (adapt `go.yml`), and a
  build/install script (adapt `pushbin.sh`).
- Port the salvageable Fzfoxide pieces (struct seed, persistence shape, DI test
  idiom); **delete** the Levenshtein module.
- **Exit:** `go build ./...` succeeds; `zjump` prints usage; `go test ./...`
  runs (even if near-empty).

### Phase 1 — Core engine (`internal/db`, `internal/config`, `internal/paths`)
- `Dir`/`Database`, versioned format (O-1), atomic writes, `score`, `age`,
  `dedup`, the matcher, and the candidate stream + filter pipeline.
- `_ZJUMP_*` env-var reading/validation and data-dir resolution.
- **Exit:** the always-on unit suite is green — scoring buckets, matcher parity
  table (A-2), aging/prune (A-4), save/reload round-trip, atomic-write behavior.
- **Covers:** R-DB-*, R-MATCH-*, R-ENV-*.

### Phase 2 — Non-interactive commands (`add`, `query`, `remove`) + error model
- Subcommand tree (O-5), flag parsing, the query dispatch (default/`--list`),
  `--exclude`/`--base-dir`/`--all`/`--score`, lazy deletions, and the
  `internal/errs` SilentExit + broken-pipe + causal-chain machinery.
- **Exit:** `zjump add/query/remove` behave per spec against a real DB;
  `zjump query --list | head` exits 0 silently (A-7).
- **Covers:** R-ADD-*, R-QRY-* (non-fzf), R-RM-*, R-ERR-*.

### Phase 3 — `init` for zsh + bash (non-fzf parts)
- `internal/shell` with `go:embed` + `text/template`; render `z`/`zi` functions,
  the `pwd` helper, hooks (zsh `precmd`/`chpwd`; bash `PROMPT_COMMAND` + emulated
  pwd-diff), `_ZJUMP_ECHO`/`_ZJUMP_RESOLVE_SYMLINKS` wiring, `--cmd`/`--no-cmd`/
  `--hook`.
- **Exit:** `eval "$(zjump init zsh|bash)"` in a real shell tracks directories
  and `z <kw>` jumps (A-1). Shell tests gated behind a build tag.
- **Covers:** R-INIT-1..8.

### Phase 4 — fzf integration (`query -i`, `zi`)
- `internal/fzf`: subprocess, NUL/tab record protocol, default args + preview
  pane, `_ZJUMP_FZF_OPTS` override, exit-code mapping incl. silent-130. Wire the
  `zi` shell function.
- **Exit:** interactive selection returns a path; Ctrl-C → silent 130 (A-5).
- **Covers:** R-FZF-1..6, R-QRY-4.

### Phase 5 — `edit` UI
- Interactive fzf browser; hidden `increment`/`decrement`/`delete`/`reload`
  subcommands; the full key-binding set; always-on preview; the insert-if-missing
  fallback. Apply deviation D-3 (re-sort after mutating reloads).
- **Exit:** `edit` mutates the DB via key bindings and re-dumps (A-6).
- **Covers:** R-EDIT-*.

### Phase 6 — Interactive tab-completion (bash, zsh)
- Space-Tab fuzzy completion: the DSR escape-sequence redraw trick (bash/zsh),
  ZLE widget for zsh, `complete -F` for bash; non-interactive fallback to native
  directory completion.
- **Exit:** Space-Tab completion invokes zjump+fzf in real bash/zsh (gated tests).
- **Covers:** R-INIT-9.

### Phase 7 — Polish
- Optional `doctor` (R-INIT-10), README build/install/usage, deviations
  documented (D-1..D-5), final gated test pass, parity table (DESIGN.md §12)
  status updated.

---

## 6. Testing strategy

Mirror zoxide's split (ARCHITECTURE.md §10, DESIGN.md §11):

- **Always-on, dependency-free** (`go test ./...`): DB round-trip, scoring
  buckets, aging/prune, dedup, atomic-write, the matcher parity table, config
  parsing/validation, error mapping. This is the default suite (A-8).
- **Build-tag gated** (e.g. `//go:build shelltests`): tests that shell out to
  real `bash`, `zsh`, and `fzf` — rendered-template execution, hook registration,
  `z`/`zi` behavior, tab-completion, and the fzf exit-code paths. CI installs
  these interpreters and runs the gated suite; ordinary contributors run only the
  default suite.
- **Table-driven** everywhere (extend the Fzfoxide idiom), with an injectable
  data-dir and clock so time-bucket and aging logic are deterministic.

---

## 7. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Matcher edge cases (overlap, anchoring) diverge from zoxide | Port zoxide's ~14 test cases verbatim as the parity table before writing prod code (TDD). |
| fzf version drift / missing flags | Enforce/document fzf ≥ 0.51.0 (R-FZF-6); fail clearly when absent. |
| Escape-sequence tab-completion is terminal-fiddly | Isolate in Phase 6, behind gated tests in real shells; degrade gracefully if unsupported. |
| Atomic-write semantics differ on macOS vs Linux | Use `CreateTemp`+`Sync`+`Rename` (portable); test on both if possible. |
| Silent-exit/broken-pipe missed at a write site | Route **all** stdout writes through one helper in `internal/errs`; test with `| head`. |
| Deviation drift from spec | Every deviation carries a `D-n` ID, a code comment, and a README note. |

---

## 8. Future work (recorded)

- **F-1 (user-requested):** a `db.zo → zjump` migration script. The recommended
  binary format (§4) makes this a straight format-to-format conversion: read
  zoxide's v3 bincode `Vec<Dir>` (fixint LE, per ARCHITECTURE.md §3), map each
  `{path, rank, last_accessed}` into zjump's layout, write via the normal atomic
  save. Ship as `zjump` subcommand or a standalone script — decide when scoped.
- **F-2** fish + remaining shells for `init`.
- **F-3** the `import` subcommand (six sources).
- **F-4** Windows / cross-platform (PowerShell, `cygpath`, UNC, `which.exe`).
- **F-5** optional `_ZO_*` env-var fallback for drop-in familiarity.
