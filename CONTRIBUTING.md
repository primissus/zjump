# Contributing to zjump

Thanks for your interest in contributing! This guide covers setup, conventions,
and the pull-request process.

## Quick start

```sh
git clone https://github.com/primissus/zjump.git
cd zjump
make build          # build to ./zjump
make test          # fast, dependency-free tests
make test-all      # all tests including shell/git integration
make lint          # gofmt + go vet
```

**Prerequisites:** Go 1.23+. For the integration test suite you'll also need
`bash`, `zsh`, `fzf` (v0.51.0+), and `git` on your `PATH`.

See [`docs/development.md`](./docs/development.md) for the full environment
guide.

---

## Finding something to work on

- Browse [open issues](https://github.com/primissus/zjump/issues) for bugs or
  features tagged `help wanted` or `good first issue`.
- If you have an idea that's not already tracked, **open an issue first** so we
  can discuss scope and approach before you invest time in code.

---

## Conventions

### Go style

- Write **idiomatic Go**. Follow standard project layout (`cmd/`, `internal/`).
- **Standard library first.** zjump has zero third-party dependencies. The
  entire project is the stdlib plus `text/template` and `go:embed` (both std
  lib). Any new dependency must be justified against the alternatives in
  `ARCHITECTURE.md §12`.
- **Tests alongside code.** Write tests in `_test.go` files in the same package,
  not as a follow-up.

### The D-n deviations

zjump has five deliberate, intentional deviations from zoxide (D-1..D-5). They
are described in the [README](./README.md#deliberate-deviations-from-zoxide).
Every one carries its `D-n` ID in a code comment. **Do not remove these
comments, and do not change the behavior they document** without discussion.

### CLI parity

Keep behavior compatible with the parity table in `DESIGN.md §12`. Any
deliberate deviation must be called out and justified, and given its own `D-n`
ID.

### Scope discipline

The build scope is defined in [`REQUIREMENTS.md`](./REQUIREMENTS.md). The
following items are **out of scope** unless explicitly re-scoped:

- N-1: `import` subcommand
- N-2: Shells other than bash/zsh
- N-3: Byte-compatibility with zoxide's `db.zo`
- N-4: Windows support
- N-5: Edit-distance (Levenshtein) matching

If you want to work on something outside this scope, open an issue first.

---

## Pull-request process

1. **Fork** the repo and create a branch:
   ```sh
   git checkout -b fix/my-feature
   ```

2. **Write code and tests** in the same commit. Keep commits focused — one
   logical change per commit.

3. **Run the full test suite and lint:**
   ```sh
   make test-all
   make lint
   ```

4. **Squash or rebase** your branch so it's clean and readable. Keep commit
   messages in the imperative mood (`Add ...`, `Fix ...`).

5. **Open a PR** against `main`. Reference any related issue. Describe what
   changed and **why**.

6. **Respond to review feedback.** Push fixes as new commits (the maintainer may
   squash-merge).

---

## Commit-message style

- Imperative mood: `Add worktree --all-repos flag`, not `Added worktree...`.
- Short subject line (≤50 chars), blank line, then a body if the "why" isn't
  obvious.
- Prefix with a conventional-commits scope when natural: `feat(list): add --json
  flag`, `fix(db): handle empty path`, `docs: update README`, `test: add zsh
  completion tests`.
- `chore: bump version to <version>` is the reserved format for version bumps.

---

## Reporting bugs

Open a [GitHub issue](https://github.com/primissus/zjump/issues/new). Include:

- zjump version (`zjump --version`)
- Shell and version (`bash --version`, `zsh --version`)
- OS (macOS / Linux distro)
- Steps to reproduce
- Expected vs. actual behavior
- If possible, the output of `zjump query --list --score` on a failing query

---

## Credit

zjump is a from-scratch Go reimplementation inspired by, and designed to be
behaviorally compatible with, [zoxide](https://github.com/ajeetdsouza/zoxide)
by Ajeet D'Souza. All credit for the original design and algorithm goes to that
project; zjump is not affiliated with it.