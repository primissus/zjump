# PROGRESS-GIT — live checklist for [`PLAN-GIT.md`](./PLAN-GIT.md)

Status values: `todo` → `in-progress` → `done` (add `blocked(<why>)` when stuck).
Update the row when the work **and its §9 tests** land; keep notes short and
link commits where useful. Do not mark a phase done until `go test ./...` is
green (plus `-tags shelltests` for Phases 5–6).

Last updated: 2026-08-29 (typed-entries port onto feat-merged `main`; all rows
reset to `todo`; `(port)` marks rows whose work differs from the git-entries
implementation; rows marked `out of scope` are not part of this port).

## Phase 1 — Format & kinds

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-DB-0 | v1 fixture from current writer → `internal/db/testdata/v1.db` (FIRST commit) | todo | 172B, 4 entries, 2 multibyte paths; fixture copied from git-entries (port) |
| R2-DB-1 | `Kind` type, `Dir.Kind`/`Dir.Name`, mutator kinds, dedup keys | todo | Add/AddUpdate take kind; findPath skips aliases; dedup keys on (isAlias, name/path) (port: no PutAlias/TouchAlias/RemoveAlias/FindAlias — A-1) |
| R2-DB-2 | Format v2 serialize/deserialize, strict validation | todo | version→2; per-entry kind+name; strict bad-kind/name-mismatch/truncation rejects; `TestFormatV2*` |
| R2-DB-3 | v1 read-compat + one-way upgrade, fixture-driven tests | todo | v1 decode→KindDir/""; v2 on first dirty save; `TestV1FixtureUpgrade`, `TestLoadV1WithoutDirtyDoesNotRewrite` |
| R2-DB-4 | Alias cull/prune exemption; dir→repo upgrade rule | todo | Age rescales-but-keeps aliases; stream never deletes aliases; upgradeKind in mutators; `TestAgingSkipsAliasCull`, `TestLazyPruneSkipsAlias` (port: decoder-spec-faithful only — A-5) |

## Phase 2 — Repo indexing

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-IDX-1 | repo-root detection (`.git` dir or file) | todo | single Lstat, no subprocess; symlink `.git` rejected; lives in `internal/git` (port: no `internal/gitx` — A-2); `TestRepoRootDetectsGitDirAndGitFile` |
| R2-IDX-2 | `add` auto-typing to repo | todo | `add` sets KindRepo via IsRepoRoot; `TestAddUpgradesDirToRepo`, `TestAddNeverDowngradesRepo` (port: edit dump stays — no aliases in DB — A-1) |
| R2-IDX-3 | `zjump index` command (walk/skip rules, upsert, summary) | todo | depth/hidden/symlink/no-descent/exclude rules; aging+single save; stderr summary; `TestIndexWalk*`, `TestIndexRespectsExcludeGlobs` |

## Phase 3 — `--type` query surface

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-TYPE-1 | `--type` flag parsing/validation on `query` | todo | validateType; `invalid type: {v}`; worktree accepted, enumeration deferred to Phase 5 |
| R2-TYPE-2 | Kind filtering; default = dir+repo; `any`; existing tests unmodified | todo | StreamOptions.WithKinds; default dir+repo; existing query/e2e tests pass unmodified (G-5); `TestQueryTypeFilter` |
| R2-TYPE-3 | `--type alias` name-matching mode | todo | alias resolved against `internal/alias` store by name, output target path (port: store, not DB — A-1) |
| R2-TYPE-4 | Alias fast path | todo | out of scope for this port — feat's `resolveAlias` stays untouched (A-1); no `aliasFastPath`/`TouchAlias` |

## Phase 4 — Aliases

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-AL-1 | `zjump alias add/rm/list`, name validation, replace-on-re-add | todo | out of scope — feat's alias CLI/UX already shipped (A-1) |
| R2-AL-2 | `remove` cannot touch aliases (guarantee + tests) | todo | out of scope — aliases live in a separate store (A-1) |

## Phase 5 — Worktrees & branches

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-WT-1 | porcelain parser extension (bare/detached/locked/prunable) | todo | extend existing `internal/git` Worktree+parseWorktrees (port: no `internal/gitx` — A-2); `TestPorcelainParser` |
| R2-WT-2 | `--type worktree` pipeline (lazy, dedup, 50-repo cap, 3-field records) | todo | lazy repo stream, keyword-on-worktree-path, cross-repo dedup, cap 50, 3-field fzf record (field-2 select); `TestWorktreeDedupAcrossRepos/RepoCapAt50/GitFailureSkipsRepoSilently/FirstAndExclude`, *(shell)* `TestWorktreeEndToEnd` |
| R2-BR-1 | `zjump branch` (marker field, fast path, fzf exit codes) | todo | out of scope — feat's `branch` unchanged (§5.5 excluded) |

## Phase 6 — Shell & docs

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-SH-1 | `{{cmd}}w`/`{{cmd}}b`/`{{cmd}}r` in bash+zsh templates | todo | out of scope — shell surface unchanged (A-4) |
| R2-SH-2 | shelltests end-to-end for new functions | todo | out of scope — shell surface unchanged (A-4) |
| R2-DOC-1 | README + AGENTS.md scope updates | todo | README: `--type`, `index`, typed entries, v2 one-way upgrade note, D-6; AGENTS.md D-1..D-6 (port: five deviations → six — A-7) |
