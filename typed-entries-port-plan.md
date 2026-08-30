# Typed entries port — handoff plan

Port git-entries' unique capabilities onto the feat-merged `main`, without a
raw git merge of `origin/git-entries` (that merge was attempted and aborted:
parallel implementations collide semantically, not textually).

## Goal

Land three capabilities from `origin/git-entries` on top of the current `main`
(= merged `feat/aliases-git-jumps`, commit `0e39216`):

1. `query --type dir|repo|worktree|alias|any` — kind-filtered query surface
2. `zjump index [--max-depth N] [--score S] <ROOT>...` — bulk repo scanner
3. Typed DB entries (format v2, one-way v1 upgrade) + live worktree
   enumeration for `--type worktree` (never stores worktrees)

## Current state

- `main` @ `0e39216` "Merge feat/aliases-git-jumps…" — all tests green:
  `go test ./...` and `go test -tags shelltests ./...` (verified after merge).
- `origin/git-entries` @ `8591e00` — fully implemented (all PROGRESS-GIT rows
  done), but NOT mergeable as-is (module path `zjump` vs
  `github.com/primissus/zjump`, incompatible `Add` signature, conflicting
  command surface).
- Current DB format: v1 (magic `ZJDB`, u32 version, u64 count; per entry:
  u64 pathlen, path, f64 rank, u64 last_accessed) in `internal/db/format.go`.
- Aliases already shipped on main live in a **separate store**
  (`internal/alias`, `<data-dir>/aliases`) with CLI `zjump alias [<name> <dir>]`,
  `-d/--delete`, `--pick`.
- `internal/git/git.go` already has porcelain worktree parsing
  (`Worktrees`, `parseWorktrees`, detached handling; `bare` currently ignored).

## Authoritative specs

- **`PLAN-GIT.md` on `origin/git-entries`** (rev 2, all design decisions final)
  is the spec for every behavior below: §2 kinds, §3 identity/dedup, §4 format
  v2, §5.1/5.3 CLI, §6 worktree enumeration. Read it via
  `git show origin/git-entries:PLAN-GIT.md`.
- Repo-wide ground truth: `AGENTS.md`, `ARCHITECTURE.md`, `DESIGN.md` (read
  before coding), and the existing `PLAN.md`/`REQUIREMENTS.md`.
- §9 of PLAN-GIT names the required tests per phase; the source test files
  exist on `origin/git-entries` (adapt, don't copy-paste blindly — see A-3).

## Decisions (locked — do not relitigate)

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

## Out of scope

PLAN-GIT Phase 4 (alias subcommands), §5.5 `zjump branch` checkout picker,
§7 shell functions/completions, cross-repo branch jumps, remote branches,
submodule indexing, v2→v1 downgrade, any new `_ZJUMP_*` env var.

## Steps

### 1. Materialize the spec + progress tracker

```sh
git show origin/git-entries:PLAN-GIT.md    > PLAN-GIT.md
git show origin/git-entries:PROGRESS-GIT.md > PROGRESS-GIT.md
```

- Prepend to `PLAN-GIT.md` a short `# PORT ADAPTATIONS` section listing
  A-1..A-8 (the file is the authoritative spec; the adaptation list keeps it
  accurate for this codebase).
- Reset every PROGRESS-GIT row to `todo`, adding a note `(port)` where the
  work differs from the git-entries implementation.
- Commit: `docs: materialize PLAN-GIT/PROGRESS-GIT for typed-entries port`.
- **Verify:** files in tree, `go test ./...` still green.

### 2. DB fixture (R2-DB-0)

- `git show origin/git-entries:internal/db/testdata/v1.db > internal/db/testdata/v1.db`
  (172 B, 4 entries, 2 multi-byte paths; generated by the same v1 writer both
  branches descend from).
- Confirm current code decodes it (this holds until Step 3 changes the code —
  do the check now, the upgrade test in Step 3 locks it in).
- Commit the bytes alone: `test: add v1 DB fixture from git-entries branch`.

### 3. Typed entries + format v2 (PLAN-GIT Phase 1)

In `internal/db/`:

- `Kind` type (`KindDir=0`, `KindRepo=1`, `KindAlias=2` per spec, though
  writer never emits 2), `Dir.Kind`, `Dir.Name` (always empty on this port).
- `Add`/`AddUpdate` gain a `kind Kind` param; `AddUnchecked` keeps its
  signature (internal). Update **all** call sites:
  `internal/cli/add.go:96`, `edit.go:67,72`, `worktree.go:187`,
  `gitpick.go:127`, plus `internal/db/db_test.go` — pass `KindDir` except
  where Step 4 upgrades to `KindRepo`.
- Identity/dedup per §3: sort key `(isAlias, key)`; dir+repo merge rule
  (sum rank, max last_accessed, `KindRepo` wins — `upgradeKind`); alias merge
  rule kept for decode-faithfulness.
- `upgradeKind` in-place dir→repo upgrade; never downgrade.
- Format v2 in `internal/db/format.go`: bump `formatVersion` to 2, append
  `kind` u8 + `name` len+bytes after the existing triple; strict validation
  per §4; v1 read-compat (all entries → `KindDir`, `""`), first dirty save
  rewrites v2, load-alone does not rewrite (D-4); "unsupported version"
  message updated to "supports 1, 2".
- Alias exemptions per spec (aging cull, lazy prune) — spec-faithful even
  though nothing writes aliases yet.
- Tests per §9 Phase 1: `TestFormatV2RoundTrip`, `TestFormatV2Rejects*`,
  `TestV1FixtureUpgrade` (decodes `testdata/v1.db`, dirty save re-reads as
  v2), `TestLoadV1WithoutDirtyDoesNotRewrite`, `TestDedupMergesDirAndRepoToRepo`,
  `TestAgingSkipsAliasCull`, `TestLazyPruneSkipsAlias`. Adapt from
  `origin/git-entries:internal/db/{format_v2_test,kinds_test,db_test}.go`.
- Commit; **verify:** `go build ./... && go vet ./... && go test ./...` green
  (existing db tests must pass unmodified where behavior is unchanged).

### 4. Repo detection + `index` (PLAN-GIT Phase 2)

- `internal/git/git.go`: add subprocess-free repo-root detection —
  `IsRepoRoot(path) bool`: one `os.Lstat` on `<path>/.git`, true iff it is a
  directory **or** a regular file (worktree marker); symlinks rejected. Mark
  with D-6. No git subprocess.
- `internal/cli/add.go`: after resolve, if `IsRepoRoot(resolved)`, pass
  `KindRepo` (in-place upgrade preserves rank/last_accessed — spec §2).
- `internal/cli/index.go` (new): `zjump index [--max-depth N] [--score S]
  <ROOT>...` exactly per §5.3 — walk rules (root = depth 0, hidden dirs
  skipped except root arg, symlinks never followed, no descent into detected
  repos, unreadable dirs skipped silently), upsert via `AddUpdate(path, S,
  now, KindRepo)` honoring `_ZJUMP_RESOLVE_SYMLINKS` and
  `_ZJUMP_EXCLUDE_DIRS` and `\n`/`\r` rejection, one aging pass, single save,
  stderr summary `indexed {N} repositories under {M} roots`. Errors:
  `index: at least one root directory is required`, `not a directory: {root}`.
- Wire `case "index"` into `internal/cli/cli.go` through the existing
  `extractGlobalFlags`/`rest` pattern (NOT git-entries' raw `args`), add help
  text in the same style as other commands (feat has per-command help
  consts — follow `internal/cli/add.go`).
- Tests per §9 Phase 2: `TestRepoRootDetectsGitDirAndGitFile` (fake `.git`
  dir/file/symlink fixtures), `TestAddUpgradesDirToRepo`,
  `TestAddNeverDowngradesRepo`, `TestIndexWalkDepthLimit`, `...SkipsHidden`,
  `...SkipsSymlinks`, `...NoDescentIntoRepo`, `...RespectsExcludeGlobs` —
  temp trees with fake `.git` markers, no real git. Adapt from
  `origin/git-entries:internal/cli/index_test.go` and
  `internal/gitx/gitx_test.go`.
- Commit; **verify:** `go test ./...` green.

### 5. `--type` query surface (PLAN-GIT Phase 3)

- `internal/cli/query.go`: add `--type` flag, `validateType` with error
  `invalid type: {value}`; accepted: `dir`, `repo`, `worktree`, `alias`,
  `any` (worktree defers to Step 6).
- `internal/db/stream.go`: add `WithKinds(...Kind)` to `StreamOptions` +
  `filterByKinds`; **default (no flag) = KindDir+KindRepo** so all existing
  query/e2e/shell tests pass unmodified (G-5 proof).
- Type semantics per §5.1 + A-1:
  - `dir` → KindDir only, match on path
  - `repo` → KindRepo only, match on path
  - `alias` → alias store, match on **name** (right-to-left matcher, name as
    single component), rows show target path, `--base-dir` on target path,
    `-s` no-op
  - `any` → DB kinds (path) + store aliases (name); no git subprocesses
  - omitted → dir+repo + existing alias fast path (feat's `resolveAlias`
    stays untouched)
- `--type` combines with `-i`/`-l`/`-s`/`--exclude`/`--base-dir`/`--all` per
  existing semantics.
- Tests per §9 Phase 3: `TestQueryTypeFilter` (each value + `any` + invalid
  error), plus the existing query test files passing unmodified. Alias fast
  path tests from git-entries (rank bump, exclude fallthrough) are **not**
  ported — store has no rank.
- Commit; **verify:** `go test ./...` green.

### 6. `--type worktree` live enumeration (PLAN-GIT Phase 5, R2-WT)

- `internal/git/git.go`: extend `Worktree` with `Bare bool`; set it in
  `parseWorktrees` (bare blocks skipped at enumeration time, per §6 step 3;
  `locked`/`prunable` remain ignored — existing default branch does this).
- `internal/cli/worktree.go` (or a new `internal/cli/typeworktree.go` —
  follow repo layout preference, keep it cohesive with the existing
  `worktree.go`): implement §6 exactly:
  1. Stream KindRepo entries best-score-first (`WithKinds(KindRepo)`),
     keyword filter **disabled** at this stage, existence+lazy pruning on
     repo paths.
  2. Per repo: `git -C {repo} worktree list --porcelain` via existing
     `internal/git.Worktrees`; any failure → skip silently, no stderr.
  3. Skip bare worktrees; detached label `[detached]`.
  4. Keyword-match on worktree **path**; `--exclude` string-compare on
     worktree path; cross-repo dedup by path, first occurrence wins.
  5. Cap at 50 repos (`maxWorktreeRepos`); default mode stops at first
     surviving candidate (lazy subprocesses).
  6. Output: default/`--list` path only (score prefix if `-s`, score = owning
     repo's decayed score); `--interactive` = 3-field NUL records
     `{score:>6.1}\t{path}\t[{branch}]` with `--delimiter=\t --nth=2`,
     selection prints **field 2** (NOT the existing "strip 7 chars" logic).
  7. Ordering: repos by frecency, worktrees in git output order.
  8. **Zero git subprocesses unless `--type worktree` was requested.**
- Tests per §9 Phase 5: `TestPorcelainParser` (fixture text: normal, detached,
  bare-skipped, locked/prunable ignored, multiple blocks — adapt to the
  extended `Worktree`), `TestWorktreeDedupAcrossRepos`,
  `TestWorktreeRepoCapAt50`, `TestWorktreeGitFailureSkipsRepoSilently`; plus
  shelltest `TestWorktreeEndToEnd` (real repo + `git worktree add`; default/
  `-l`/`-i` field-2 extraction). Adapt from
  `origin/git-entries:internal/cli/worktree_test.go`,
  `internal/gitx/worktree_test.go`.
- Commit; **verify:** `go test ./...` and `go test -tags shelltests ./...`
  green.

### 7. Docs (PLAN-GIT Phase 6, R2-DOC-1)

- README: `--type` section, `index` command, typed-entries concept, format v2
  one-way upgrade note, D-6 in the deviations list.
- AGENTS.md: scope note (extension scope gains typed entries; D-1..D-6).
- Update PROGRESS-GIT rows done→ with commit links.
- Commit; **verify:** both test suites green; rendered help text matches
  README (feat has tests that check help/env-list sync — keep them passing).

### 8. Version bump

- `internal/cli/cli.go`: `var Version = "0.8.0"`.
- Commit `chore: bump version to 0.8.0`. Do NOT tag/push.

## Pitfalls

- Ported files import `zjump/internal/...` — rewrite to
  `github.com/primissus/zjump/internal/...` (A-3). The build error
  `package zjump/internal/config is not in std` is this bug.
- feat's `cli.go` wraps dispatch in `extractGlobalFlags` (`--debug`/
  `--log-file`) — wire `index` through `rest`, not `args`.
- feat's `parseArgs` permutes flags; use feat's helper, not git-entries'.
- The `Add`/`AddUpdate` signature change breaks 4 non-test call sites +
  `db_test.go` — fix them in the same commit (Step 3).
- feat's stream has no `WithKinds` — add it without altering existing
  filter semantics; the untouched existing tests are the proof.
- `--type worktree -i` fzf records have 3 fields with a different delimiter
  contract than `query -i` — do not reuse the 7-char strip.
- feat's `edit` `dumpEntries` dumps all dirs; aliases are not in the DB on
  this port, so no alias filtering is needed there (unlike git-entries).
- Broken-pipe-tolerant output for any new list-style output; follow
  `internal/cli/list.go`'s existing pattern.
- Never run git during plain `query`, `add`, `index` (index uses only
  `os.Lstat` detection; worktree enumeration is the sole subprocess user).

## Final verification

1. `go build ./... && go vet ./... && go test ./...`
2. `go test -tags shelltests ./...`
3. `git log --oneline main..HEAD` shows the step commits; working tree clean.
