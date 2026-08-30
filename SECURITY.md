# Security Policy

## Reporting a vulnerability

If you discover a security vulnerability in zjump, please report it
**responsibly**:

1. **Do not open a public GitHub issue.**

2. Email **security@primissus.dev** with a description of the vulnerability,
   steps to reproduce, and any proof-of-concept code.

3. Include the zjump version (`zjump --version`), your OS, and shell
   (`bash --version` / `zsh --version`).

4. You will receive an acknowledgement within 48 hours, and we will
   coordinate a fix and disclosure timeline with you.

## Scope

This policy covers the `zjump` binary and its shell-integration scripts
(bash/zsh). The security boundary is what a user running zjump exposes:

- The on-disk database file (read/write on every invocation).
- The shell-integration script sourced by the user's shell.
- Shell-outs to external utilities (`fzf`, `git`).

## Out of scope

- Vulnerabilities in dependencies of `fzf` or `git` (report to those projects).
- Issues in shells other than bash and zsh (explicitly out of scope).
- Windows support (explicitly out of scope, D-5).
- Issues requiring an attacker who already has write access to the user's
  database file (the file lives in the user's own data directory).

## Supported versions

Only the [latest release](https://github.com/primissus/zjump/releases) is
supported with security fixes.