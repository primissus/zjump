#!/usr/bin/env bash
# Install the zjump binary. Usage:
#   scripts/install.sh [VERSION] [DEST_DIR]
#   scripts/install.sh --build [DEST_DIR]
#
# Without --build, downloads a pre-built binary from GitHub Releases.
# With --build, builds from source (needs Go 1.23+).
#
# VERSION defaults to the latest release.
# DEST_DIR defaults to $HOME/.local/bin (must be on your PATH).
set -euo pipefail

PROJECT="primissus/zjump"
DEST=""
VERSION=""
BUILD_MODE=false

# Parse arguments.
while [[ $# -gt 0 ]]; do
    case "$1" in
        --build)
            BUILD_MODE=true
            shift
            ;;
        -*)
            echo "Unknown flag: $1" >&2
            echo "Usage: $0 [--build] [VERSION] [DEST_DIR]" >&2
            exit 1
            ;;
        *)
            if [[ -z "$VERSION" ]]; then
                VERSION="$1"
            elif [[ -z "$DEST" ]]; then
                DEST="$1"
            else
                echo "Too many arguments: $1" >&2
                exit 1
            fi
            shift
            ;;
    esac
done

DEST="${DEST:-$HOME/.local/bin}"

if $BUILD_MODE; then
    echo "Building from source..."
    cd "$(dirname "$0")/.."
    go build -o zjump ./cmd/zjump
    echo "Installing to ${DEST}/zjump..."
    mkdir -p "${DEST}"
    install -m 0755 zjump "${DEST}/zjump"
    rm -f zjump
else
    if [[ -z "$VERSION" ]]; then
        echo "Fetching latest release..."
        VERSION=$(curl -fsSL "https://api.github.com/repos/${PROJECT}/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
        if [[ -z "$VERSION" ]]; then
            echo "Could not determine latest version. Try specifying one, or use --build." >&2
            exit 1
        fi
        echo "Latest release: ${VERSION}"
    fi

    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) echo "Unsupported architecture: ${ARCH}" >&2; exit 1 ;;
    esac

    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$OS" in
        darwin|macos) OS="darwin" ;;
        linux) OS="linux" ;;
        *) echo "Unsupported OS: ${OS}" >&2; exit 1 ;;
    esac

    TARBALL="zjump_${VERSION#v}_${OS}_${ARCH}.tar.gz"
    URL="https://github.com/${PROJECT}/releases/download/${VERSION}/${TARBALL}"

    echo "Downloading ${TARBALL}..."
    TMPDIR=$(mktemp -d)
    curl -fsSL "$URL" -o "${TMPDIR}/${TARBALL}" || {
        echo "Download failed. The release may not include a build for ${OS}/${ARCH}." >&2
        echo "Try: $0 --build ${DEST}" >&2
        rm -rf "$TMPDIR"
        exit 1
    }

    echo "Installing to ${DEST}/zjump..."
    mkdir -p "${DEST}"
    tar -xzf "${TMPDIR}/${TARBALL}" -C "${TMPDIR}"
    install -m 0755 "${TMPDIR}/zjump" "${DEST}/zjump"
    rm -rf "$TMPDIR"
fi

echo "Done. Add the shell hook to your rc file, e.g.:"
echo '  eval "$(zjump init bash)"   # or: zjump init zsh'
