# Jump Test Cases

Test scenarios for the four jump types: directory (frecency), alias, branch,
and worktree. Each refers to concrete paths from `~/src/` where applicable.
Run with `go test -tags shelltests ./test/` for git-dependent cases.

---

## 1. Directory jump (`zz <keywords>`) — existing parity

| ID | Scenario | Command | Expected | Example from ~/src |
|---|---|---|---|---|
| D-01 | Single keyword, top match | `zz zjump` | cd into `~/src/zjump` (highest-frecency match for "zjump") | `~/src/zjump` |
| D-02 | Two ordered keywords | `zz loft req` | cd into `~/src/loftia-requests` | `~/src/loftia-requests` |
| D-03 | No args → home | `zz` | `cd ~` | — |
| D-04 | Previous directory | `zz -` | `cd $OLDPWD` | — |
| D-05 | Existing local dir | `zz ./internal` | cd into `./internal` (no DB query) | `~/src/zjump/internal` |
| D-06 | `--` passthrough | `zz -- ~/src/zjump` | cd directly | — |
| D-07 | Case-insensitive match | `zz ZJUMP` | matches `~/src/zjump` | — |
| D-08 | `zzi` interactive pick | `zzi veg` | fzf picker with matching entries | `vegapunk` etc. |
| D-09 | No match | `zz xyznonexistent` | error: "no match found" | — |

---

## 2. Alias jump (`z <alias>`, `zz -a <name> <dir>`)

| ID | Scenario | Command | Expected | Example mapping |
|---|---|---|---|---|
| A-01 | Create alias | `zz -a proj ~/src/zjump` | Silent success; `zjump alias` shows `proj\t~/src/zjump` | zjump as `proj` |
| A-02 | Jump via alias | `zz proj` (after A-01) | cd into `~/src/zjump` | — |
| A-03 | Alias beats frecency | DB has `~/src/proj-tools`, alias `proj` → `~/src/zjump`. `zz proj` | cd into alias target, not frecency match | — |
| A-04 | Local dir beats alias | `./proj` exists as a real directory. `zz proj` from parent | cd into `./proj` (superset-of-cd rule) | — |
| A-05 | Multi-keyword skips alias | `zz proj tools` (2 keywords) | frecency query, never alias | — |
| A-06 | `zzi <alias>` skips alias | `zzi proj` | fzf picker with frecency matches, alias ignored | — |
| A-06b | `zzi -a` scored alias pick | `zzi -a` | fzf picker of aliases, each prefixed by its target's frecency, best first | alias with top-scored target |
| A-07 | `query --list <alias>` skips alias | `zjump query --list proj` | frecency matches listed, alias ignored | — |
| A-08 | Dangling alias → clear error | Delete alias target dir. `zz lostalias` | `alias "lostalias" points to a directory that no longer exists: <path>` | — |
| A-09 | Alias == $PWD | `zz here` where alias `here` → current dir | `you are already in the only match` | — |
| A-10 | Overwrite alias | `zz -a x ~/src/zjump` then `zz -a x ~/src/loftia` | `zz x` → `~/src/loftia` | — |
| A-11 | Delete alias | `zjump alias -d proj` | removed; `zz proj` falls through to frecency | — |
| A-12 | Delete nonexistent | `zjump alias -d nosuch` | `alias not found: nosuch` | — |
| A-13 | Invalid name: empty | `zjump alias "" ~/src/zjump` | error | — |
| A-14 | Invalid name: slash | `zjump alias foo/bar ~/src/zjump` | error | — |
| A-15 | Invalid name: dash | `zjump alias -x ~/src/zjump` | error | — |
| A-16 | Invalid target: file | `zjump alias key ~/src/zjump/go.mod` | `not a directory: <path>` | — |
| A-17 | List aliases | `zjump alias` | sorted `name<TAB>path` lines | — |
| A-18 | Alias survives DB reset | Delete DB, keep alias file | `zz proj` still works (aliases live outside DB) | — |
| A-19 | Case-sensitive alias | `zz PROJ` vs `zz proj` | Only `proj` matches; `PROJ` falls to frecency | — |

---

## 3. Branch jump (`zz -b <branch> [repo]`)

Based on `~/src/git-interact`, `~/src/loftia`, `~/src/vegapunk`.

| ID | Scenario | Command | Expected |
|---|---|---|---|
| B-01 | Jump to main branch (CWD's repo) | `cd ~/src/git-interact && zz -b main` | cd into `~/src/git-interact` |
| B-02 | Jump to feature branch (CWD's repo) | `cd ~/src/loftia && zz -b authz-m1/phase-1-fga-schema` | cd into `~/src/loftia-worktree` |
| B-03 | Jump with repo via DB keyword | `zz -b main git-interact` | resolves `git-interact` via frecency → finds main worktree → prints `~/src/git-interact` |
| B-04 | Jump with repo via DB keyword (feature) | `zz -b p11-branch-view gi` | resolves `gi` via frecency → `~/src/git-interact` → finds p11-branch-view → prints `~/src/git-interact-p11` |
| B-05 | Branch miss | `zz -b nonexistent` (from any repo) | `branch not checked out in any worktree: nonexistent` + hint listing available branches |
| B-06 | No git dir | `zz -b main /tmp` | `not a git repository: /tmp` |
| B-07 | git not installed | (uninstall git) `zz -b main` | `could not find git, is it installed?` |
| B-08 | Detached HEAD worktree | `git-interact` has only 2 non-detached | `zz -b` skips detached entries (no branch to match) |
| B-09 | Repo via DB + miss | `zz -b staging loftia-stale-kw` (keywords don't match) | `no match found for repo: <keywords>` |

---

## 4. Worktree jump (`zz -w <name> [repo]`)

| ID | Scenario | Command | Expected |
|---|---|---|---|
| W-01 | Match by basename | `cd ~/src/git-interact && zz -w git-interact-p11` | cd into `~/src/git-interact-p11` |
| W-02 | Match by branch (no basename match) | `cd ~/src/loftia && zz -w authz-m1/phase-1-fga-schema` | cd into `~/src/loftia-worktree` (basename "loftia-worktree" ≠ branch, but branch matches) |
| W-03 | Match by basename (preferred over branch) | `zz -w loftia` from loftia repo | `~/src/loftia` (basename match beats branch match if both exist) |
| W-04 | Ambiguous basename → error | Two worktrees with same basename | `ambiguous worktree: <name> (matches: <path1>, <path2>)` |
| W-05 | Name hint lists available | `zz -w nowhere` from git-interact repo | `no worktree found: nowhere` + `available worktrees: git-interact (main), git-interact-p11 (p11-branch-view)` |
| W-06 | Worktree via DB keyword | `zz -w loftia-worktree loft` | resolves `loft` → `~/src/loftia` → lists worktrees → matches basename → prints `~/src/loftia-worktree` |
| W-07 | Detached worktree skipped for branch match | `zz -w friendly-meninsky` (vegapunk detach) | basename match on `.claude/worktrees/friendly-meninsky-9af94a` — unlikely to match unless exact basename; branch match skipped (detached) |
| W-08 | Not a git repo | `zz -w foo /tmp` | `not a git repository: /tmp` |

---

## 5. Integration & edge cases

| ID | Scenario | Command | Expected |
|---|---|---|---|
| I-01 | Hook tracks alias target | `zz -a src ~/src`, `zz src` | Hook fires on arrival → `~/src` added to DB with incremented rank |
| I-02 | Hook tracks branch target | `zz -b main` | Hook adds the target worktree dir to the DB |
| I-03 | Hook tracks worktree target | `zz -w git-interact-p11` | Hook adds the target worktree dir to the DB |
| I-04 | `--cmd zz` rename | `eval "$(zjump init bash --cmd zz)"` | All four jump types work under `zz` prefix |
| I-05 | `--no-cmd` still has __zjump_z | `eval "$(zjump init bash --no-cmd)"` | `__zjump_z` has all flag dispatch but no-alias `zz` wrapper |
| I-06 | Broken pipe on alias list | `zjump alias | head -1` | Silent exit 0 (same pipe-tolerance as query) |
| I-07 | `_ZJUMP_RESOLVE_SYMLINKS` on alias path | Set to 1, `zz -a` with symlinked target | Resolves symlink before storing |

---

## 6. `zjump list` overview (zjump-only extension)

`zjump list` is not a jump — it's a combined view of the four tracked entities.
There is no zoxide equivalent; zoxide's closest behavior is `zoxide query --list`
(a flag, not a subcommand). See [`REQUIREMENTS.md` §2.13](../REQUIREMENTS.md) for
the full `R-LIST-*` requirements.

| ID | Scenario | Command | Expected |
|---|---|---|---|
| L-01 | Bare list ≈ `query --list` | `zjump list` | DIRECTORIES section only, best-first, PATH column; ALIASES/BRANCHES/WORKTREES absent |
| L-02 | Score column opt-in | `zjump list --score` | DIRECTORIES gains a SCORE column with `%6.1f` formatting (clamped to `9999.0`) |
| L-03 | Empty DB | `zjump list` (fresh DB) | DIRECTORIES header + a single `(none)` row |
| L-04 | Add ALIASES section | `zjump list --aliases` | DIRECTORIES + ALIASES sections both printed, separated by a blank line |
| L-05 | Empty requested section | `zjump list --aliases` (no aliases configured) | ALIASES header + `(none)` row (NOT omitted — distinguishes "asked for, none" from "suppressed") |
| L-06 | Suppress DIRECTORIES | `zjump list --aliases --no-dirs` | Only the ALIASES section appears; DIRECTORIES header absent entirely |
| L-07 | Add BRANCHES (CWD repo) | `cd ~/src/zjump && zjump list --branches` | BRANCHES header carries `(repo: ~/src/zjump)` hint; columns are BRANCH/PATH (no REPO column) |
| L-08 | Add WORKTREES (CWD repo) | `cd ~/src/zjump && zjump list --worktrees` | WORKTREES header carries repo hint; columns are BASENAME/BRANCH/PATH; detached worktrees labeled `(detached)` |
| L-09 | Out-of-repo, no keywords | `cd /tmp && zjump list --branches --worktrees` | Section headers print with `(no git repository)` marker + `(none)` body (NOT an error) |
| L-10 | Repo via DB keywords | `zjump list --branches zjump` | Best DB match for "zjump" → `git.RepoRoot` → BRANCHES of that repo; errors `no match found for repo` if no DB hit (mirrors `zjump branch`) |
| L-11 | All-repos scan | `zjump list --branches --worktrees --all-repos` | Scans every DB entry containing `.git`; BRANCHES/WORKTREES gain a REPO leading column; dedups by canonical main-checkout path |
| L-12 | JSON output, minimal | `zjump list --json` | JSON object with `directories` key only; other section keys absent (omitempty) |
| L-13 | JSON output, requested-empty | `zjump list --json --aliases` (no aliases) | `aliases: []` present (NOT `null`); consumers can distinguish "asked for, none" from "not requested" |
| L-14 | JSON output, all sections | `zjump list --json --aliases --branches --worktrees` | All four keys present; row objects carry documented field names |
| L-15 | D-4: clean listing no rewrite | `stat db` before & after `zjump list` | File mtime unchanged when no lazy-deletions fire (Save is a no-op when not dirty) |
| L-16 | Broken pipe | `zjump list \| head -1` | Silent exit 0 (same pipe-tolerance as `query --list`) |

---

## 7. Worktree indexing (zjump-only extension, 2026-08-05)

Worktree paths get written into the frecency DB (seed-once, rank 1.0) so they
are jumpable by plain `zz <keyword>` before ever being visited. See
[`REQUIREMENTS.md` §2.12](../REQUIREMENTS.md) for the full `R-GIT-9`..`R-GIT-11`,
`R-ADD-10`, and `R-ENV-8` requirements.

| ID | Scenario | Command | Expected |
|---|---|---|---|
| G-01 | `zz -W` outside a repo | `cd /tmp && zz -W` (repo tracked in DB) | fzf lists that repo's worktrees; labels carry `[repo: <basename>]` |
| G-02 | `zz -W` inside a repo | `cd ~/src/repo && zz -W` | Still lists worktrees of ALL DB-known repos (ignores cwd) |
| G-03 | `--all` single-offer fast path | One tracked repo, one worktree | Path printed directly (no fzf) |
| G-04 | `zz -W` seeds worktrees | Track repo, run `zz -W`, then `zjump query <sibling-basename>` | Sibling worktree is in DB and jumpable by frecency |
| G-05 | `zz -w` seeds siblings | `cd main && zz -w main`, then `zjump query feature-x` | The never-visited feature worktree is seeded and jumpable |
| G-06 | `zz -b` seeds siblings | `cd main && zz -b main`, then `zjump query feature-x` | Same seeding as G-05 (branches ride on worktree paths) |
| G-07 | Auto-index off (default) | `zjump add main` (no env var) | Sibling worktrees NOT added to DB |
| G-08 | Auto-index on | `_ZJUMP_AUTO_INDEX_DIRECTORY=1 zjump add main` | Repo's worktrees seeded once (rank 1.0) |
| G-09 | Seed-once: no rank inflation | Repeat `_ZJUMP_AUTO_INDEX_DIRECTORY=1 zjump add main` | Sibling rank stays 1.0 (score 4.0), only real visits grow it |
| G-10 | Dedup across tracked worktrees | Track both main + feature, `zjump worktree --all` | Repo's worktrees emitted once (canonical main-checkout dedup) |
| G-11 | Best-effort indexing | `zz -w` with broken `_ZJUMP_DATA_DIR` | Lookup still prints/jumps; indexing silently skipped |

---

## Quick smoke test

One-liner to seed the DB and verify all four jump types work end-to-end:

```sh
# Seed frecency DB with common projects
for d in ~/src/zjump ~/src/loftia ~/src/vegapunk ~/src/git-interact ~/src/ai-gateway; do
  zjump add "$d"
done

# 1. Directory jump
zz zjump         # → ~/src/zjump

# 2. Alias
zz -a zp ~/src/zjump
zz zp            # → ~/src/zjump

# 3. Branch (from git-interact repo CWD)
cd ~/src/git-interact
zz -b p11-branch-view   # → ~/src/git-interact-p11

# 4. Worktree (from loftia repo CWD)
cd ~/src/loftia
zz -w loftia-worktree   # → ~/src/loftia-worktree

# 5. Combined overview (zjump-only — no zoxide equivalent)
zjump list --score --aliases --branches --worktrees
```
