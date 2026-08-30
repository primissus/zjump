# PORT ADAPTATIONS

This file is the authoritative spec for the typed-entries port onto the
feat-merged `main`. The decisions below (A-1..A-8) adapt the git-entries
implementation to this codebase; they are locked and not to be relitigated.

- **A-1 — Aliases stay in the separate store.** feat's alias CLI/UX is shipped
  and unchanged. No `KindAlias` in the DB, no `PutAlias`/`TouchAlias`, no
  `alias add|rm|list` subcommands. `--type alias` and the alias part of
  `--type any` resolve against `internal/alias`, matched **by name**, rows show
  the target path, `--base-dir` filters on the target path, `-s` is a no-op
  (the store has no frecency). Existence pruning never touches store aliases.
- **A-2 — No `internal/gitx` package.** Extend the existing `internal/git`.
- **A-3 — Module path is `github.com/primissus/zjump`.** Every file ported
  from git-entries must have its `zjump/internal/...` imports rewritten.
- **A-4 — Shell surface unchanged.** No `zw`/`zr`/`zb` functions; `zz -a/-b/
  -w/-W` wiring in the templates is out of scope and stays byte-identical
  (aside from any help-text touch-ups if trivially required).
- **A-5 — Format v2 per PLAN-GIT §4**, but the writer only ever emits kind
  0 (dir) or 1 (repo). Decoder stays spec-faithful: accepts kind 0–2 with
  strict validation (kind>2, name/kind mismatch, truncation → hard error),
  so files from a future alias-in-DB design still decode.
- **A-6 — feat's worktree/branch seeding stays.** `_ZJUMP_AUTO_INDEX_DIRECTORY`,
  `seedWorktrees`, `worktree --all`, `zz -W` keep current behavior. The new
  live enumeration is additive (`query --type worktree`).
- **A-7 — D-6 markers** at every new user-facing entry point per PLAN-GIT
  §10: `--type` parsing, `index` command, repo-root detection helper, new
  template touch points (if any). Update README's deviations section and
  AGENTS.md (five deviations → six).
- **A-8 — Version bump to 0.8.0** at the end (`v0.7.0` tag already exists on
  main's history). Do **not** tag or push — releases are the user's step.

---

# PLAN-GIT — Typed entries: repos, worktrees, branches, aliases (rev 2)

Implementation plan for extending zjump beyond plain directory jumping with
four new capabilities:

1. **Jump between git worktrees** of indexed repositories.
2. **Checkout branches** of the current repository interactively.
3. **Index repositories** (auto-detect on the cd hook + manual bulk scan).
4. **Aliases** — user-named shortcuts that resolve to paths.

Progress is tracked in [`PROGRESS-GIT.md`](./PROGRESS-GIT.md) — one checklist
row per requirement ID below.

This plan extends the committed scope of `PLAN.md` / `REQUIREMENTS.md`; it does
not modify or supersede them. Everything in the original parity scope
(add/query/remove/init/edit, bash+zsh, fzf) must keep working unchanged.

---

## 0. Instructions for the implementing agent

**Read order before writing any code:** `AGENTS.md` → `ARCHITECTURE.md` →
`DESIGN.md` → this file. Repo conventions in `AGENTS.md` apply in full
(idiomatic Go, stdlib-first, tests land with the code).

**Working rules:**

- Execute phases in order (§8). One phase per commit (or PR). Run
  `go test ./...` green before every commit; run
  `go test -tags shelltests ./...` additionally after Phases 5 and 6.
- After each requirement ID lands (code + its named tests from §9), update its
  row in `PROGRESS-GIT.md` (`todo` → `in-progress` → `done`, short note).
- Every design decision is already made in this document. **Do not invent
  behavior.** If this spec is silent on something you need, stop and ask —
  do not guess, do not add scope.
- All new user-facing surface carries a `D-6` marker comment at its entry
  point (see §10), following the existing D-n convention.
- Do not modify `PLAN.md`, `REQUIREMENTS.md`, `ARCHITECTURE.md`, or
  `DESIGN.md`.

## 1. Decisions (interview outcomes, 2026-07-11 — all final)

| # | Decision | Choice |
|---|---|---|
| G-1 | Storage for typed entries | **Single database, per-entry `kind` field.** Format version bump; one frecency engine, one aging pass, one query pipeline filtered by `--type`. |
| G-2 | Worktree jumping | **Live discovery.** `kind=repo` entries are the source of truth; worktrees enumerated at query time via `git worktree list --porcelain`, never stored. |
| G-3 | Branch checkout | **Switch in current repo only.** New command lists current repo's branches through fzf, prints selection; generated shell function performs `git switch`. |
| G-4 | Repository indexing | **Auto-detect on hook + manual scan.** `add` upgrades entries to `kind=repo` when the path is a repo root; `zjump index <root>` bulk-scans. |
| G-5 | Default type | **Plain directories.** With no `--type`, all existing invocations behave exactly as today (dir+repo entries by path). New behavior opt-in via arguments. |
| G-6 | Alias rank | **Bump on use.** An alias fast-path hit increments the alias's rank and sets `last_accessed = now`, then saves (query becomes mutating on alias hits only). |
| G-7 | Alias fast-path trigger | **Single keyword; exact match wins, else case-sensitive prefix match, best score wins** (full rules in §5.2). |
| G-8 | Alias + `--exclude` | **Fall through.** Candidates whose target equals `--exclude` are removed from the fast-path candidate set; if none remain, continue into normal keyword matching. |
| G-9 | `zjump branch` current branch | **Show all, mark current.** Marker lives in a separate fzf display field; command output is always the clean branch name (§5.6). |
| G-10 | Completions for new shell functions | **Cut.** `zw`/`zb`/`zr` get the shell's native default completion only. No DSR/Space-Tab machinery. May be a future phase; not this plan. |

## 2. Entry kinds

```go
type Kind uint8

const (
    KindDir   Kind = 0 // plain directory (existing behavior; the default)
    KindRepo  Kind = 1 // root of a git repository (has .git)
    KindAlias Kind = 2 // user-named shortcut; Name field set
)
```

Rules:

- **No `KindWorktree` exists.** Worktrees are derived at query time (§6).
- **Repo root detection:** a path is a repo root iff `<path>/.git` exists as a
  directory **or** a regular file (the file form is what a linked worktree
  root has, so visited worktree roots become `KindRepo` too and can enumerate
  their siblings). One `os.Lstat` on `<path>/.git`; no `git` subprocess.
- **`KindAlias`:** `Name` non-empty, `Path` is the target. Rank/last_accessed
  apply normally (G-6) but aliases are **exempt from both deletion paths**:
  the aging pass's `< 1.0 → delete` cull skips them (their rank still gets
  rescaled by the same factor), and query's lazy existence/TTL pruning never
  touches them. Aliases are deleted **only** by `zjump alias rm`.
- **Multiple aliases may point at the same target path** — alias uniqueness
  is by name, never by path. Same path may simultaneously exist as a
  dir/repo entry and as any number of alias targets; these are independent
  entries.
- **dir→repo upgrade:** `add` on a path stored as `KindDir` but detected as a
  repo root flips the entry to `KindRepo` in place (rank, last_accessed
  preserved). Never downgrade repo→dir — a vanished `.git` leaves the typing;
  worktree enumeration skips it silently (§6) and lazy pruning removes truly
  dead paths.

## 3. Identity, dedup, and mutators

**Entry identity key:**

- `KindDir` / `KindRepo`: the `path` string. A dir and repo entry never
  coexist for the same path (the upgrade rule collapses them).
- `KindAlias`: the `name` string. Re-adding an existing name replaces its
  target path in place (rank, last_accessed preserved).

**Dedup pass** (`Database.Dedup`) sort key, exactly:
`(isAlias, key)` where `isAlias` = (kind == KindAlias), `key` = name for
aliases, path otherwise. Adjacent-equal merge rules:

- Equal non-alias keys: merged rank = sum, `last_accessed` = max,
  kind = `KindRepo` if either is repo (upgrade wins).
- Equal alias keys (same name): merged rank = sum, `last_accessed` = max,
  path = the entry with the greater `last_accessed` wins (latest target).

**Mutators:** `Add`, `AddUpdate` gain a `kind Kind` parameter; every existing
call site passes `KindDir` (behavior byte-identical to today for dirs).
Alias creation/replacement goes through a dedicated `PutAlias(name, path,
now)`; alias rank bump on use goes through `TouchAlias(name, now)`
(rank += 1.0, `last_accessed = now`, sets dirty).

**`zjump remove` and `zjump edit` never touch aliases:**

- `remove` matches `KindDir`/`KindRepo` entries by path only (existing exact
  → lexical-resolve fallback logic unchanged). It cannot delete an alias.
- `edit`'s fzf list contains only `KindDir`/`KindRepo` entries (aliases are
  filtered out of the reload dump); its `delete`/`increment`/`decrement`
  path arguments therefore never collide with alias entries. Row format is
  unchanged (`score\tpath`).

## 4. Database format v2 (R2-DB)

Bump `formatVersion` in `internal/db/format.go` from `1` to `2`. Magic and
header layout unchanged apart from the version value. Per-entry layout
appends two fields **after** the existing triple:

| Field | Size | Encoding |
|---|---|---|
| path len | 8 | u64 LE |
| path | var | raw UTF-8 |
| rank | 8 | f64 LE |
| last_accessed | 8 | u64 LE |
| **kind** | **1** | **u8 — must be 0, 1, or 2** |
| **name len** | **8** | **u64 LE — must be 0 unless kind=2** |
| **name** | **var** | **raw UTF-8** |

Decode strictness (all are hard `"could not deserialize database: corrupted
data"` errors, matching existing philosophy): kind byte > 2; `name_len > 0`
with kind ≠ 2; kind = 2 with `name_len == 0`; truncation anywhere. The
existing 32 MiB size guard and magic/version checks stay.

**Version handling on read:**

- v2 → decode as above.
- v1 → decode with the v1 layout; every entry becomes
  `kind=KindDir, name=""`. In-memory only; the first **dirty** save rewrites
  the file as v2 (loading alone does not rewrite — D-4 preserved). One-way
  upgrade; no downgrade path (README documents this).
- any other version → existing "unsupported version" error, updated to say
  "supports 1, 2".

**Fixture (R2-DB-0, do FIRST):** before modifying any format code, generate
`internal/db/testdata/v1.db` using the current, unmodified v1 writer (a small
throwaway test or `go run` snippet that saves a DB with ≥3 entries, varied
ranks/timestamps, at least one multi-byte-UTF-8 path). Commit the bytes. The
v1-upgrade tests decode this file — never bytes produced by the new code.

## 5. CLI surface

### 5.1 `--type` flag on `query` (R2-TYPE)

```
zjump query [--type dir|repo|worktree|alias|any] [KEYWORDS]...
```

| Value | Candidate set | Keywords match against |
|---|---|---|
| *(omitted)* | `KindDir` + `KindRepo` (today's behavior, G-5) — plus the alias fast path, §5.2 | path |
| `dir` | `KindDir` only | path |
| `repo` | `KindRepo` only | path |
| `alias` | `KindAlias` only | **name** (standard right-to-left matcher; name treated as a single path component). Output/`-l`/`-i` rows show the **target path** (record format identical to normal query). |
| `worktree` | live enumeration, §6 | worktree path |
| `any` | all DB kinds (dir+repo by path, alias by name) — **no** worktree enumeration (`any` stays a pure DB query, no git subprocesses) | per kind as above |

Invalid `--type` value → error `invalid type: {value}`. The flag combines
with `-i`/`-l`/`-s`/`--exclude`/`--base-dir`/`--all` per existing semantics
(for `alias`, `--base-dir` filters on the target path; `--all` is a no-op for
aliases since they're exempt from existence pruning anyway).

### 5.2 Alias fast path in default mode (G-6/G-7/G-8)

Runs **before** the normal match stream, only when **all** of:

- no `--type` given, and not `--list` / `--interactive`
  (first-match mode only);
- exactly **one** keyword.

Algorithm, exactly:

1. Collect alias candidates: exact — every alias whose name equals the
   keyword byte-for-byte (at most one, names are unique); else prefix —
   every alias whose name starts with the keyword (case-sensitive,
   byte-wise prefix).
2. Drop candidates whose **target path** equals the `--exclude` value
   (string compare, verbatim — same as existing `--exclude` semantics).
3. Exact candidate survives → it wins. Else pick the surviving prefix
   candidate with the **highest decayed score** (`Dir.Score(now)`, existing
   formula); ties broken by lexicographically smaller name (determinism).
4. Winner → `TouchAlias` (G-6: rank += 1.0, last_accessed = now), save DB,
   print the **target path** (with score prefix if `-s`), done.
5. No winner (no aliases, all excluded) → fall through to the normal
   dir+repo match stream as if the fast path didn't exist (G-8).

### 5.3 `zjump index` (R2-IDX)

```
zjump index [--max-depth N] [--score S] <ROOT>...
```

- Walk each root top-down. Depth: the root itself is depth 0; do not descend
  below `--max-depth` (default 3).
- At each directory: repo root (per §2 detection) → record it, **do not
  descend into it** (nested repos/submodules out of scope). Otherwise recurse
  into subdirectories, **skipping**: hidden directories (name starts with
  `.`; the root argument itself is exempt from this rule), symlinks (never
  followed), and unreadable directories (permission errors skipped silently).
- Each recorded repo: resolve the path exactly as `add` does
  (`_ZJUMP_RESOLVE_SYMLINKS` honored), skip if it matches an
  `_ZJUMP_EXCLUDE_DIRS` glob or contains `\n`/`\r`, else
  `AddUpdate(path, S, now, KindRepo)` with `S` = `--score` (default 1.0).
  Same semantics as `add`, so re-indexing refreshes frecency (+1 per run) by
  design.
- After all roots: aging pass, single save. Summary to **stderr**:
  `indexed {N} repositories under {M} roots`. Root that doesn't exist or
  isn't a directory → error (consistent with `add`'s "not a directory").

### 5.4 `zjump alias` (R2-AL)

```
zjump alias add <NAME> [PATH]   # PATH defaults to cwd
zjump alias rm <NAME>
zjump alias list [--score]
```

- **Name validation** (error `invalid alias name: {name}` on violation):
  non-empty; no `/`, no whitespace, no `\n`/`\r`; must not start with `-`;
  must not be exactly `.`, `..`, `-`, or `~` (the shell function's cd-idiom
  branches consume those before zjump ever sees them).
- `add`: resolve PATH per `_ZJUMP_RESOLVE_SYMLINKS` (same as `zjump add`);
  error `not a directory: {path}` if not a directory at add time. New name →
  insert with rank 1.0, `last_accessed = now`. Existing name → replace target
  in place (rank, last_accessed preserved). Save.
- `rm`: exact name match; error `alias not found: {name}` otherwise. Save.
- `list`: rows `name\tpath` (plus leading score column when `--score`),
  sorted best decayed score first, **no dedup by target** (multiple aliases
  to one dir all show). Broken-pipe-tolerant stdout like other list output.

### 5.5 `zjump branch` (R2-BR)

```
zjump branch [PATTERN]
```

1. `git rev-parse --is-inside-work-tree` — on failure or non-`true` output,
   error `not inside a git repository`.
2. List local branches:
   `git for-each-ref refs/heads --sort=-committerdate --format=%(refname:short)`.
   Empty list (unborn HEAD, no branches) → error `no branches found`.
3. Current branch: `git branch --show-current` (empty on detached HEAD —
   then no branch is marked). If present, move it to the **front** of the
   list.
4. **Fast path:** if PATTERN given and it substring-matches (case-sensitive)
   exactly one branch name, print that name and exit — no fzf spawned
   (scriptable; works without fzf installed).
5. Otherwise spawn fzf (reuse `internal/fzf` wiring: exit-code table,
   SilentExit on 130, "could not find fzf" error). Records, NUL-terminated:
   `{marker}\t{branch}` where marker is `*` for the current branch, one
   space otherwise. Args include `--delimiter=\t --nth=2 --read0 --exit-0`
   plus the shared cosmetic defaults used by `query -i`; PATTERN (if given)
   seeds `--query={PATTERN}`.
6. Selection: print **field 2** (the clean branch name) to stdout. The
   binary never runs `git switch` — the shell function does (§7).

### 5.6 Unchanged commands

`add` gains no user-facing flag (kind auto-detected, G-4). `remove`, `edit`,
`init` flags unchanged (`init` templates change, §7). All six
`_ZJUMP_*` env vars unchanged; **no new env vars**.

## 6. Worktree enumeration (R2-WT)

`--type worktree` pipeline, exactly:

1. Stream `KindRepo` entries best-score-first through the existing stream
   machinery (keyword filter **disabled** at this stage — keywords apply to
   worktree paths in step 4; existence filter and lazy pruning apply to the
   repo paths as usual).
2. Per repo, run `git -C {repo} worktree list --porcelain`. Any failure
   (git missing, not a repo anymore, nonzero exit) → skip that repo
   silently, no stderr output.
3. Parse porcelain blocks: `worktree {path}` starts a block;
   `branch refs/heads/{name}` sets its branch; a `detached` line sets label
   `detached`; a `bare` line marks the block **skipped** (bare repos are not
   jumpable); `locked`/`prunable`/other lines ignored. Main worktree is a
   normal candidate.
4. Apply the standard keyword matcher to each worktree **path**. Apply
   `--exclude` (string compare against worktree path). Dedup across repos by
   worktree path — first occurrence wins (repos already stream in
   best-score-first order).
5. Stop conditions: enumeration processes at most the first **50** repos;
   within that, default mode stops at the first surviving candidate
   (subprocesses run lazily, not all upfront).
6. Output:
   - default / `--list`: worktree **path only** per line (score prefix if
     `-s`, score = the owning repo's decayed score). Scripts consume this.
   - `--interactive`: fzf records `{score:>6.1}\t{path}\t[{branch}]`
     NUL-terminated (`[detached]` for detached; `[]` never empty — block
     always has branch or detached), with `--delimiter=\t --nth=2` (match
     path only; branch visible, not matchable). Selection prints **field 2**
     — do not reuse the existing "strip 7 chars" logic, this is a
     three-field record.
7. Ordering: repos by frecency, each repo's worktrees in git's output order.
8. **No git subprocess may run unless `--type worktree` was requested.**
   Errors "no match found" / "you are already in the only match" apply as in
   normal query.

New package `internal/gitx`: repo-root detection helper (§2), porcelain
runner+parser (§6), branch listing helpers (§5.5). All git invocations via
`os/exec` with explicit arg lists; no shell interpolation; stdlib only.

## 7. Shell integration (R2-SH)

Templates `bash.tmpl` / `zsh.tmpl` gain three functions alongside `z`/`zi`
(prefix follows `--cmd`: `--cmd j` → `jw`/`jb`/`jr`; all suppressed by
`--no-cmd`, exactly like `z`/`zi`):

| Function | No args | With args |
|---|---|---|
| `{{cmd}}w` | `zjump query --type worktree -i --exclude "$PWD"` → cd | `zjump query --type worktree --exclude "$PWD" -- "$@"` → cd |
| `{{cmd}}r` | `zjump query --type repo -i --exclude "$PWD"` → cd | `zjump query --type repo --exclude "$PWD" -- "$@"` → cd |
| `{{cmd}}b` | `branch="$(\command zjump branch)" && \command git switch "$branch"` | same with `-- "$@"` passed to `zjump branch` |

- cd/echo behavior of `{{cmd}}w`/`{{cmd}}r` mirrors `z`'s existing result
  handling (`_ZJUMP_ECHO` honored); `{{cmd}}b` echoes nothing itself (`git
  switch` already prints). fzf Ctrl-C (exit 130) propagates silently through
  `&&` — no error text, matching existing behavior.
- Unlike `z`, `{{cmd}}w`/`{{cmd}}r` with no args go interactive (no "cd home"
  analog for worktrees/repos). No `-`/`--`/existing-dir special branches —
  those belong to `z` only.
- **The cd hook is unchanged** — still `zjump add -- <pwd>`; repo detection
  lives inside `add` (G-4). Zero added per-prompt cost.
- **No completion wiring for the new functions** (G-10) — shell's native
  default completion applies. Existing `z`/`zi` completion glue untouched.

## 8. Implementation phases

### Phase 1 — Format & kinds (foundation)

| ID | Work |
|---|---|
| R2-DB-0 | Generate + commit `internal/db/testdata/v1.db` with the **current, unmodified** v1 writer (§4). Must be the first commit of the phase. |
| R2-DB-1 | `Kind` type; `Dir.Kind`/`Dir.Name`; mutator kind params; `PutAlias`/`TouchAlias`; identity/dedup rules (§3). |
| R2-DB-2 | Format v2 serialize/deserialize with strict validation (§4). |
| R2-DB-3 | v1 read-compat → in-memory upgrade, v2 on next dirty save; fixture-driven tests. |
| R2-DB-4 | Alias exemption from aging cull + lazy pruning; dir→repo in-place upgrade rule. |

### Phase 2 — Repo indexing

| ID | Work |
|---|---|
| R2-IDX-1 | `internal/gitx`: repo-root detection (`.git` dir **or** file, single Lstat). |
| R2-IDX-2 | `add` auto-types/upgrades `KindRepo` (§2); `edit` reload dump filters out aliases (§3). |
| R2-IDX-3 | `zjump index`: walk rules, skip rules, upsert, aging, stderr summary (§5.3). |

### Phase 3 — `--type` query surface

| ID | Work |
|---|---|
| R2-TYPE-1 | `--type` flag parse/validate on `query` (§5.1). |
| R2-TYPE-2 | Kind filtering in the stream; default = dir+repo; `any` = all DB kinds. Existing query tests must pass **unmodified** (G-5 proof). |
| R2-TYPE-3 | `--type alias` name-matching mode (matcher on name, output target path). |
| R2-TYPE-4 | Alias fast path per §5.2 incl. TouchAlias bump and exclude fallthrough. |

### Phase 4 — Aliases

| ID | Work |
|---|---|
| R2-AL-1 | `zjump alias add/rm/list`, name validation, replace-on-re-add (§5.4). |
| R2-AL-2 | `remove` untouched-by-alias guarantee + tests (§3). |

### Phase 5 — Worktrees & branches

| ID | Work |
|---|---|
| R2-WT-1 | `internal/gitx`: porcelain runner + parser (bare/detached/locked/prunable handling), branch listing helpers. |
| R2-WT-2 | `--type worktree` pipeline: lazy per-repo enumeration, dedup, 50-repo cap, three-field fzf records, `-i`/`-l`/`-s`/`--exclude` behavior (§6). |
| R2-BR-1 | `zjump branch`: repo check, list, current-first + marker field, single-match fast path, fzf wiring + exit codes (§5.5). |

### Phase 6 — Shell & docs

| ID | Work |
|---|---|
| R2-SH-1 | `{{cmd}}w`/`{{cmd}}b`/`{{cmd}}r` in bash+zsh templates; `--cmd`/`--no-cmd` handling; no completion wiring (G-10). |
| R2-SH-2 | shelltests-tagged end-to-end tests (§9, Phase 6 row). |
| R2-DOC-1 | README: new commands, `--type`, v2 one-way upgrade note, alias semantics (incl. multiple aliases per target). AGENTS.md scope note pointing here. |

## 9. Definition of done — named tests per phase

A phase is done only when its code **and** these tests exist and pass.
Dependency-free tests run under plain `go test ./...`; rows marked *(shell)*
require `-tags shelltests` and real `git`/`bash`/`zsh`/`fzf`.

| Phase | Required test coverage (names indicative, keep the intent exact) |
|---|---|
| 1 | `TestFormatV2RoundTrip` (all kinds, multi-byte UTF-8 path+name); `TestFormatV2RejectsBadKind`, `...RejectsNameOnDir`, `...RejectsAliasWithoutName`, truncation cases; `TestV1FixtureUpgrade` (decodes `testdata/v1.db`, all KindDir, dirty save re-reads as v2); `TestLoadV1WithoutDirtyDoesNotRewrite`; `TestDedupMergesDirAndRepoToRepo`, `TestDedupAliasByName`; `TestAgingSkipsAliasCull` (alias rank rescaled but entry kept below 1.0); `TestLazyPruneSkipsAlias`. |
| 2 | `TestRepoRootDetectsGitDirAndGitFile`; `TestAddUpgradesDirToRepo`, `TestAddNeverDowngradesRepo`; `TestIndexWalkDepthLimit`, `...SkipsHidden`, `...SkipsSymlinks`, `...NoDescentIntoRepo`, `...RespectsExcludeGlobs` (temp trees with fake `.git` markers — no real git needed); `TestEditReloadOmitsAliases`. |
| 3 | `TestQueryTypeFilter` (each `--type` value + `any` + invalid value error); existing query tests pass unmodified; `TestAliasFastPathExactWins`, `...PrefixBestScore`, `...PrefixTieLexicographic`, `...ExcludedFallsThrough`, `...MultiKeywordSkipsFastPath`, `...ListModeSkipsFastPath`, `...BumpsRankAndSaves`. |
| 4 | `TestAliasNameValidation` (table: empty, `/`, whitespace, leading `-`, `.`, `..`, `-`, `~`, valid names); `TestAliasReplaceKeepsRank`; `TestAliasRm` + not-found error; `TestAliasListOrderAndNoTargetDedup`; `TestRemoveCannotDeleteAlias`. |
| 5 | `TestPorcelainParser` (fixture text: normal, detached, bare-skipped, locked/prunable ignored, multiple blocks); `TestWorktreeDedupAcrossRepos`; `TestWorktreeRepoCapAt50`; `TestWorktreeGitFailureSkipsRepoSilently`; *(shell)* `TestWorktreeEndToEnd` (real repo + `git worktree add`, query default/`-l`/`-i` field-2 extraction), `TestBranchEndToEnd` (list order, current marker, fast path, exit 130 silent, `no branches found`, `not inside a git repository`). |
| 6 | *(shell)* template tests under real bash+zsh: `zw`/`zr` cd to selection, `zb` switches branch, `--cmd j` renames to `jw`/`jb`/`jr`, `--no-cmd` omits all three, `_ZJUMP_ECHO` honored for `zw`/`zr`; existing template tests pass unmodified. |

## 10. Deviations & out of scope

- **D-6:** zjump gains capabilities zoxide lacks (typed entries, `--type`,
  `index`, `alias`, `branch`, worktree jumps). Original zoxide-parity surface
  (DESIGN.md §12) is preserved; all new surface is additive and opt-in (G-5).
  Carry `D-6` comment markers at: `--type` parsing, alias fast path, `index`,
  `alias`, `branch` command entry points, `internal/gitx` package doc, new
  template functions.
- **Out of scope** (do not implement, do not stub): cross-repo branch jump /
  auto `git worktree add`; storing worktrees in the DB / `KindWorktree`;
  remote branches in `zjump branch`; submodule/nested-repo indexing; DB
  v2→v1 downgrade; completions for the new functions (G-10); shells beyond
  bash+zsh (N-2); any new `_ZJUMP_*` env var.
