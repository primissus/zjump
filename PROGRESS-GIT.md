# PROGRESS-GIT — live checklist for [`PLAN-GIT.md`](./PLAN-GIT.md)

Status values: `todo` → `in-progress` → `done` (add `blocked(<why>)` when stuck).
Update the row when the work **and its §9 tests** land; keep notes short and
link commits where useful. Do not mark a phase done until `go test ./...` is
green (plus `-tags shelltests` for Phases 5–6).

Last updated: 2026-07-11 (plan rev 2 — all design decisions final; no
implementation started)

## Phase 1 — Format & kinds

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-DB-0 | v1 fixture from current writer → `internal/db/testdata/v1.db` (FIRST commit) | done | 172B, 4 entries, 2 multibyte paths; throwaway generator removed |
| R2-DB-1 | `Kind` type, `Dir.Kind`/`Dir.Name`, mutator kinds, `PutAlias`/`TouchAlias`, dedup keys | done | Add/AddUpdate take kind; findPath skips aliases; RemoveAlias/FindAlias added; Dedup keys on (isAlias, name/path) |
| R2-DB-2 | Format v2 serialize/deserialize, strict validation | done | version→2; per-entry kind+name; strict bad-kind/name-mismatch/truncation rejects; `TestFormatV2*` |
| R2-DB-3 | v1 read-compat + one-way upgrade, fixture-driven tests | done | v1 decode→KindDir/""; v2 on first dirty save; `TestV1FixtureUpgrade`, `TestLoadV1WithoutDirtyDoesNotRewrite` |
| R2-DB-4 | Alias cull/prune exemption; dir→repo upgrade rule | done | Age rescales-but-keeps aliases; stream never deletes aliases; upgradeKind in mutators; `TestAgingSkipsAliasCull`, `TestLazyPruneSkipsAlias` |

## Phase 2 — Repo indexing

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-IDX-1 | `internal/gitx` repo-root detection (`.git` dir or file) | todo | |
| R2-IDX-2 | `add` auto-typing to repo; `edit` dump omits aliases | todo | |
| R2-IDX-3 | `zjump index` command (walk/skip rules, upsert, summary) | todo | |

## Phase 3 — `--type` query surface

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-TYPE-1 | `--type` flag parsing/validation on `query` | todo | |
| R2-TYPE-2 | Kind filtering; default = dir+repo; `any`; existing tests unmodified | todo | |
| R2-TYPE-3 | `--type alias` name-matching mode | todo | |
| R2-TYPE-4 | Alias fast path (exact > prefix-by-score, exclude fallthrough, rank bump) | todo | |

## Phase 4 — Aliases

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-AL-1 | `zjump alias add/rm/list`, name validation, replace-on-re-add | todo | |
| R2-AL-2 | `remove` cannot touch aliases (guarantee + tests) | todo | |

## Phase 5 — Worktrees & branches

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-WT-1 | `gitx` porcelain runner/parser + branch listing helpers | todo | |
| R2-WT-2 | `--type worktree` pipeline (lazy, dedup, 50-repo cap, 3-field records) | todo | |
| R2-BR-1 | `zjump branch` (marker field, fast path, fzf exit codes) | todo | |

## Phase 6 — Shell & docs

| ID | Work | Status | Notes |
|---|---|---|---|
| R2-SH-1 | `{{cmd}}w`/`{{cmd}}b`/`{{cmd}}r` in bash+zsh templates (no completions, G-10) | todo | |
| R2-SH-2 | shelltests end-to-end for new functions | todo | |
| R2-DOC-1 | README + AGENTS.md scope updates | todo | |
