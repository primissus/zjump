# Agent Instructions for zjump

## Project

- `zjump` is a Go reimplementation of [zoxide](https://github.com/ajeetdsouza/zoxide),
  a frecency-based directory-jumping CLI tool.
- Current status: **implemented** for the committed scope in
  [`REQUIREMENTS.md`](./REQUIREMENTS.md) (add/query/remove/init/edit, bash+zsh,
  fzf). Source lives under `cmd/zjump` and `internal/*`; `go test ./...` is the
  dependency-free suite and `go test -tags shelltests ./...` adds the real
  bash/zsh/fzf integration tests.

## Authoritative specs — read before touching anything

- `ARCHITECTURE.md` documents how upstream zoxide (Rust) is actually built:
  data storage, frecency algorithm, shell integration, process model.
- `DESIGN.md` documents zoxide's CLI surface (commands, flags, env vars,
  behaviors) that `zjump` is meant to match for parity.
- Read both in full before proposing or making any change. Treat them as
  ground truth over assumptions or general zoxide knowledge.

## Scope discipline

- The initial build scope was signed off and implemented per
  [`REQUIREMENTS.md`](./REQUIREMENTS.md) / [`PLAN.md`](./PLAN.md). Stay within
  that scope: the out-of-scope items (N-1..N-6 — `import`, non-bash/zsh shells,
  `db.zo` byte-compat, Windows, Levenshtein) remain out unless the user
  explicitly re-scopes them.
- A second, signed-off scope extension (typed entries: repos, worktrees,
  branches, aliases) is specified in [`PLAN-GIT.md`](./PLAN-GIT.md) with live
  status in [`PROGRESS-GIT.md`](./PROGRESS-GIT.md). All its design decisions
  are final; implement it exactly as written there.
- The five deliberate deviations from zoxide (D-1..D-5) are intentional; keep
  them, and keep every one carrying its `D-n` ID in a code comment and README note.

## Conventions

- Write idiomatic Go; follow standard Go project layout conventions
  (`cmd/`, `internal/`, etc.) as appropriate.
- Prefer the standard library over third-party dependencies; justify any
  new dependency against the alternatives noted in `ARCHITECTURE.md` §12.
- Keep CLI behavior compatible with the parity table in `DESIGN.md` §12;
  call out and justify any deliberate deviation.
- Write tests alongside new code, not as a separate follow-up.
