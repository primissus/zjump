# Jump Test Cases

Test scenarios for the four jump types: directory (frecency), alias, branch,
and worktree. Each refers to concrete paths from `~/src/` where applicable.
Run with `go test -tags shelltests ./test/` for git-dependent cases.

---

## 1. Directory jump (`z <keywords>`) — existing parity

| ID | Scenario | Command | Expected | Example from ~/src |
|---|---|---|---|---|
| D-01 | Single keyword, top match | `z zjump` | cd into `~/src/zjump` (highest-frecency match for "zjump") | `~/src/zjump` |
| D-02 | Two ordered keywords | `z loft req` | cd into `~/src/loftia-requests` | `~/src/loftia-requests` |
| D-03 | No args → home | `z` | `cd ~` | — |
| D-04 | Previous directory | `z -` | `cd $OLDPWD` | — |
| D-05 | Existing local dir | `z ./internal` | cd into `./internal` (no DB query) | `~/src/zjump/internal` |
| D-06 | `--` passthrough | `z -- ~/src/zjump` | cd directly | — |
| D-07 | Case-insensitive match | `z ZJUMP` | matches `~/src/zjump` | — |
| D-08 | `zi` interactive pick | `zi veg` | fzf picker with matching entries | `vegapunk` etc. |
| D-09 | No match | `z xyznonexistent` | error: "no match found" | — |

---

## 2. Alias jump (`z <alias>`, `z -a <name> <dir>`)

| ID | Scenario | Command | Expected | Example mapping |
|---|---|---|---|---|
| A-01 | Create alias | `z -a proj ~/src/zjump` | Silent success; `zjump alias` shows `proj\t~/src/zjump` | zjump as `proj` |
| A-02 | Jump via alias | `z proj` (after A-01) | cd into `~/src/zjump` | — |
| A-03 | Alias beats frecency | DB has `~/src/proj-tools`, alias `proj` → `~/src/zjump`. `z proj` | cd into alias target, not frecency match | — |
| A-04 | Local dir beats alias | `./proj` exists as a real directory. `z proj` from parent | cd into `./proj` (superset-of-cd rule) | — |
| A-05 | Multi-keyword skips alias | `z proj tools` (2 keywords) | frecency query, never alias | — |
| A-06 | `zi <alias>` skips alias | `zi proj` | fzf picker with frecency matches, alias ignored | — |
| A-07 | `query --list <alias>` skips alias | `zjump query --list proj` | frecency matches listed, alias ignored | — |
| A-08 | Dangling alias → clear error | Delete alias target dir. `z lostalias` | `alias "lostalias" points to a directory that no longer exists: <path>` | — |
| A-09 | Alias == $PWD | `z here` where alias `here` → current dir | `you are already in the only match` | — |
| A-10 | Overwrite alias | `z -a x ~/src/zjump` then `z -a x ~/src/loftia` | `z x` → `~/src/loftia` | — |
| A-11 | Delete alias | `zjump alias -d proj` | removed; `z proj` falls through to frecency | — |
| A-12 | Delete nonexistent | `zjump alias -d nosuch` | `alias not found: nosuch` | — |
| A-13 | Invalid name: empty | `zjump alias "" ~/src/zjump` | error | — |
| A-14 | Invalid name: slash | `zjump alias foo/bar ~/src/zjump` | error | — |
| A-15 | Invalid name: dash | `zjump alias -x ~/src/zjump` | error | — |
| A-16 | Invalid target: file | `zjump alias key ~/src/zjump/go.mod` | `not a directory: <path>` | — |
| A-17 | List aliases | `zjump alias` | sorted `name<TAB>path` lines | — |
| A-18 | Alias survives DB reset | Delete DB, keep alias file | `z proj` still works (aliases live outside DB) | — |
| A-19 | Case-sensitive alias | `z PROJ` vs `z proj` | Only `proj` matches; `PROJ` falls to frecency | — |

---

## 3. Branch jump (`z -b <branch> [repo]`)

Based on `~/src/git-interact`, `~/src/loftia`, `~/src/vegapunk`.

| ID | Scenario | Command | Expected |
|---|---|---|---|
| B-01 | Jump to main branch (CWD's repo) | `cd ~/src/git-interact && z -b main` | cd into `~/src/git-interact` |
| B-02 | Jump to feature branch (CWD's repo) | `cd ~/src/loftia && z -b authz-m1/phase-1-fga-schema` | cd into `~/src/loftia-worktree` |
| B-03 | Jump with repo via DB keyword | `z -b main git-interact` | resolves `git-interact` via frecency → finds main worktree → prints `~/src/git-interact` |
| B-04 | Jump with repo via DB keyword (feature) | `z -b p11-branch-view gi` | resolves `gi` via frecency → `~/src/git-interact` → finds p11-branch-view → prints `~/src/git-interact-p11` |
| B-05 | Branch miss | `z -b nonexistent` (from any repo) | `branch not checked out in any worktree: nonexistent` + hint listing available branches |
| B-06 | No git dir | `z -b main /tmp` | `not a git repository: /tmp` |
| B-07 | git not installed | (uninstall git) `z -b main` | `could not find git, is it installed?` |
| B-08 | Detached HEAD worktree | `git-interact` has only 2 non-detached | `z -b` skips detached entries (no branch to match) |
| B-09 | Repo via DB + miss | `z -b staging loftia-stale-kw` (keywords don't match) | `no match found for repo: <keywords>` |

---

## 4. Worktree jump (`z -w <name> [repo]`)

| ID | Scenario | Command | Expected |
|---|---|---|---|
| W-01 | Match by basename | `cd ~/src/git-interact && z -w git-interact-p11` | cd into `~/src/git-interact-p11` |
| W-02 | Match by branch (no basename match) | `cd ~/src/loftia && z -w authz-m1/phase-1-fga-schema` | cd into `~/src/loftia-worktree` (basename "loftia-worktree" ≠ branch, but branch matches) |
| W-03 | Match by basename (preferred over branch) | `z -w loftia` from loftia repo | `~/src/loftia` (basename match beats branch match if both exist) |
| W-04 | Ambiguous basename → error | Two worktrees with same basename | `ambiguous worktree: <name> (matches: <path1>, <path2>)` |
| W-05 | Name hint lists available | `z -w nowhere` from git-interact repo | `no worktree found: nowhere` + `available worktrees: git-interact (main), git-interact-p11 (p11-branch-view)` |
| W-06 | Worktree via DB keyword | `z -w loftia-worktree loft` | resolves `loft` → `~/src/loftia` → lists worktrees → matches basename → prints `~/src/loftia-worktree` |
| W-07 | Detached worktree skipped for branch match | `z -w friendly-meninsky` (vegapunk detach) | basename match on `.claude/worktrees/friendly-meninsky-9af94a` — unlikely to match unless exact basename; branch match skipped (detached) |
| W-08 | Not a git repo | `z -w foo /tmp` | `not a git repository: /tmp` |

---

## 5. Integration & edge cases

| ID | Scenario | Command | Expected |
|---|---|---|---|
| I-01 | Hook tracks alias target | `z -a src ~/src`, `z src` | Hook fires on arrival → `~/src` added to DB with incremented rank |
| I-02 | Hook tracks branch target | `z -b main` | Hook adds the target worktree dir to the DB |
| I-03 | Hook tracks worktree target | `z -w git-interact-p11` | Hook adds the target worktree dir to the DB |
| I-04 | `--cmd zz` rename | `eval "$(zjump init bash --cmd zz)"` | All four jump types work under `zz` prefix |
| I-05 | `--no-cmd` still has __zjump_z | `eval "$(zjump init bash --no-cmd)"` | `__zjump_z` has all flag dispatch but no-alias `z`/`zz` wrapper |
| I-06 | Broken pipe on alias list | `zjump alias | head -1` | Silent exit 0 (same pipe-tolerance as query) |
| I-07 | `_ZJUMP_RESOLVE_SYMLINKS` on alias path | Set to 1, `z -a` with symlinked target | Resolves symlink before storing |

---

## Quick smoke test

One-liner to seed the DB and verify all four jump types work end-to-end:

```sh
# Seed frecency DB with common projects
for d in ~/src/zjump ~/src/loftia ~/src/vegapunk ~/src/git-interact ~/src/ai-gateway; do
  zjump add "$d"
done

# 1. Directory jump
z zjump         # → ~/src/zjump

# 2. Alias
z -a zp ~/src/zjump
z zp            # → ~/src/zjump

# 3. Branch (from git-interact repo CWD)
cd ~/src/git-interact
z -b p11-branch-view   # → ~/src/git-interact-p11

# 4. Worktree (from loftia repo CWD)
cd ~/src/loftia
z -w loftia-worktree   # → ~/src/loftia-worktree
```
