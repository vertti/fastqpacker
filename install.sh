#!/bin/sh
set -e

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
if [ "$OS" != "linux" ] && [ "$OS" != "darwin" ]; then
    echo "Unsupported OS: $OS"
    exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

BINARY="fqpack-${OS}-${ARCH}"
RELEASES="https://github.com/vertti/fastqpacker/releases"

# Get latest version
VERSION=$(curl -fsSL "${RELEASES}/latest" -o /dev/null -w '%{url_effective}' | rev | cut -d'/' -f1 | rev)
URL="${RELEASES}/download/${VERSION}/${BINARY}"

echo "Downloading fqpack ${VERSION} for ${OS}/${ARCH}..."
curl -fsSL "$URL" -o fqpack

chmod +x fqpack

# Install to /usr/local/bin or ~/bin
if [ -w /usr/local/bin ]; then
    mv fqpack /usr/local/bin/
    echo "Installed to /usr/local/bin/fqpack"
elif [ -d "$HOME/bin" ]; then
    mv fqpack "$HOME/bin/"
    echo "Installed to ~/bin/fqpack"
else
    mkdir -p "$HOME/bin"
    mv fqpack "$HOME/bin/"
    echo "Installed to ~/bin/fqpack"
    echo "Add ~/bin to your PATH if not already there"
fi

echo "Done! Run 'fqpack -version' to verify."
