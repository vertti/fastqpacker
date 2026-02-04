#!/usr/bin/env bash
#
# Install benchmark tools on macOS
# Usage: ./scripts/install-benchmark-tools-macos.sh
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TOOLS_DIR="$SCRIPT_DIR/../tools"
mkdir -p "$TOOLS_DIR"

echo "=== Installing benchmark tools for macOS ==="
echo ""

# --- Homebrew tools ---
echo "Installing tools via Homebrew..."
brew install pigz zstd p7zip xz boost 2>/dev/null || true

# --- repaq (from source) ---
if [[ ! -x "$TOOLS_DIR/repaq" ]]; then
    echo "Building repaq from source..."
    TMPDIR=$(mktemp -d)
    trap "rm -rf $TMPDIR" EXIT

    git clone --depth 1 https://github.com/OpenGene/repaq.git "$TMPDIR/repaq"
    (cd "$TMPDIR/repaq" && make -j$(sysctl -n hw.ncpu))
    cp "$TMPDIR/repaq/repaq" "$TOOLS_DIR/"
    echo "  Installed repaq to $TOOLS_DIR/repaq"
else
    echo "  repaq already installed"
fi

# --- DSRC (from source) ---
if [[ ! -x "$TOOLS_DIR/dsrc" ]]; then
    echo "Building DSRC from source..."
    TMPDIR=$(mktemp -d)
    trap "rm -rf $TMPDIR" EXIT

    git clone --depth 1 https://github.com/refresh-bio/DSRC.git "$TMPDIR/DSRC"
    (cd "$TMPDIR/DSRC/src" && make bin \
        CXXFLAGS="-O2 -m64 -DNDEBUG -I/opt/homebrew/include" \
        DEP_LIB_DIRS="-L/opt/homebrew/lib" \
        DEP_LIBS="-lboost_thread -lpthread -lz")
    cp "$TMPDIR/DSRC/src/dsrc" "$TOOLS_DIR/"
    echo "  Installed DSRC to $TOOLS_DIR/dsrc"
else
    echo "  DSRC already installed"
fi

echo ""
echo "=== Installed tools ==="
echo ""
echo "Homebrew: pigz, zstd, 7z, xz"
echo "From source (in $TOOLS_DIR):"
[[ -x "$TOOLS_DIR/repaq" ]] && echo "  - repaq"
[[ -x "$TOOLS_DIR/dsrc" ]] && echo "  - DSRC"
echo ""
echo "To run benchmarks: ./scripts/benchmark-paper-comparison.sh"
echo "First download test data: ./scripts/download-benchmark-data.sh"
