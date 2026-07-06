#!/usr/bin/env bash
# Build and install the zjump binary. Usage: scripts/install.sh [DEST_DIR]
# DEST_DIR defaults to /usr/local/bin (install there may require sudo).
set -euo pipefail

cd "$(dirname "$0")/.."

DEST="${1:-/usr/local/bin}"

echo "Building zjump..."
go build -o zjump ./cmd/zjump

echo "Installing to ${DEST}/zjump..."
install -d "${DEST}"
install -m 0755 zjump "${DEST}/zjump"

echo "Done. Add the shell hook to your rc file, e.g.:"
echo '  eval "$(zjump init bash)"   # or: zjump init zsh'
