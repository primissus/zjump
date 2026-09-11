# Agent Instructions for zjump

## Project

- `zjump` is a Go reimplementation of [zoxide](https://github.com/ajeetdsouza/zoxide),
  a frecency-based directory-jumping CLI tool.
- Current status: **implemented** for the committed scope in
  [`REQUIREMENTS.md`](./REQUIREMENTS.md) (add/query/remove/init/edit, bash+zsh,
  fzf), plus the **extension scope** (aliases, git branch/worktree jumps
  (2026-07-24 re-scope), `zjump list` — a zjump-only combined view of
  directories, aliases, branches, and worktrees — and worktree/branch
  indexing (2026-08-05): `worktree --all`/`zz -W`, `_ZJUMP_AUTO_INDEX_DIRECTORY`,
  and `-w`/`-b` seeding). `zjump update` (2026-08-06) self-updates the binary
  from GitHub Releases by downloading the matching goreleaser archive,
  verifying its SHA256, and atomically replacing the running binary. The
  typed-entries port (2026-08-29) adds typed DB entries (dir/repo kinds, format
  v2 with one-way v1 upgrade), `query --type dir|repo|worktree|alias|any`,
  and `zjump index` — spec in [`PLAN-GIT.md`](./PLAN-GIT.md). Keyword tab
  completion (2026-09-10): `zz <word><TAB>` completes from current dir +
  aliases + indexed dirs, `<TAB><TAB>` lists match/fuzzy/indexed sections,
  backed by the hidden `zjump complete` subcommand (spec `R-COMPLETE-*`). Source
  lives under `cmd/zjump` and `internal/*`;
  `go test ./...` is the dependency-free suite and
  `go test -tags shelltests ./...` adds the real bash/zsh/fzf/git integration
  tests.

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
- The six deliberate deviations from zoxide (D-1..D-6) are intentional; keep
  them, and keep every one carrying its `D-n` ID in a code comment and README note.

## Conventions

- Write idiomatic Go; follow standard Go project layout conventions
  (`cmd/`, `internal/`, etc.) as appropriate.
- Prefer the standard library over third-party dependencies; justify any
  new dependency against the alternatives noted in `ARCHITECTURE.md` §12.
- Keep CLI behavior compatible with the parity table in `DESIGN.md` §12;
  call out and justify any deliberate deviation.
- Write tests alongside new code, not as a separate follow-up.

## Versioning & releases

- The canonical version lives in `internal/cli/cli.go` (`const Version`).
- When bumping:
  1. Update `const Version` in `internal/cli/cli.go`.
  2. Commit with message `chore: bump version to <version>`.
  3. Tag the commit: `git tag -a v<version> -m "v<version>"`.
  4. Push the tag: `git push origin v<version>`.
  The tag triggers `.github/workflows/release.yml`, which runs goreleaser
  to cross-compile and publish GitHub Release assets.
- goreleaser reads the version from the git tag, not from the Go constant.
  Keep both in sync. See `.goreleaser.yml` for the build matrix.
- The release workflow also publishes to the Homebrew tap
  [`primissus/homebrew-tap`](https://github.com/primissus/homebrew-tap)
  (`brew tap primissus/tap && brew install zjump`): goreleaser's
  `homebrew_casks` block regenerates `Casks/zjump.rb` (version + SHA256s)
  and commits it with the `HOMEBREW_TAP_GITHUB_TOKEN` secret. The cask file
  is auto-generated — never hand-edit it; verify the tap commit landed
  after tagging, and re-run the workflow (re-tag won't work) if it didn't.
- `go install github.com/primissus/zjump/cmd/zjump@latest` always works;
  pre-built tarballs are available on the GitHub Releases page.
