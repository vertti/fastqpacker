#!/bin/sh
set -eu

# Colors (with fallbacks for non-color terminals)
BOLD="$(tput bold 2>/dev/null || printf '')"
GREY="$(tput setaf 8 2>/dev/null || printf '')"
GREEN="$(tput setaf 2 2>/dev/null || printf '')"
YELLOW="$(tput setaf 3 2>/dev/null || printf '')"
RED="$(tput setaf 1 2>/dev/null || printf '')"
RESET="$(tput sgr0 2>/dev/null || printf '')"

info() {
    printf '%s\n' "${BOLD}${GREY}>${RESET} $*"
}

success() {
    printf '%s\n' "${GREEN}✓${RESET} $*"
}

warn() {
    printf '%s\n' "${YELLOW}!${RESET} $*" >&2
}

error() {
    printf '%s\n' "${RED}✗${RESET} $*" >&2
    exit 1
}

# Check for required commands
has() {
    command -v "$1" >/dev/null 2>&1
}

has curl || has wget || error "curl or wget is required"

# Detect OS
detect_os() {
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        linux*)  echo "linux" ;;
        darwin*) echo "darwin" ;;
        *)       error "Unsupported OS: $os" ;;
    esac
}

# Detect architecture
detect_arch() {
    arch=$(uname -m)
    case "$arch" in
        x86_64)  echo "amd64" ;;
        aarch64) echo "arm64" ;;
        arm64)   echo "arm64" ;;
        *)       error "Unsupported architecture: $arch" ;;
    esac
}

# Download file
download() {
    url="$1"
    output="$2"
    if has curl; then
        curl -fsSL "$url" -o "$output"
    else
        wget -qO "$output" "$url"
    fi
}

# Detect shell profile file
detect_profile() {
    case "${SHELL:-}" in
        */zsh)  echo "${ZDOTDIR:-$HOME}/.zshrc" ;;
        */bash)
            if [ -f "$HOME/.bashrc" ]; then
                echo "$HOME/.bashrc"
            elif [ -f "$HOME/.bash_profile" ]; then
                echo "$HOME/.bash_profile"
            else
                echo "$HOME/.bashrc"
            fi
            ;;
        */fish) echo "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish" ;;
        *)      echo "$HOME/.profile" ;;
    esac
}

# Add directory to PATH in shell profile
add_to_path() {
    dir="$1"
    profile=$(detect_profile)

    # Check if already in PATH
    case ":$PATH:" in
        *":$dir:"*) return 0 ;;
    esac

    # Check if profile already has this directory
    if [ -f "$profile" ] && grep -q "$dir" "$profile" 2>/dev/null; then
        return 0
    fi

    info "Adding $dir to PATH in $profile"

    case "${SHELL:-}" in
        */fish)
            mkdir -p "$(dirname "$profile")"
            printf '\nfish_add_path %s\n' "$dir" >> "$profile"
            ;;
        *)
            printf '\nexport PATH="%s:$PATH"\n' "$dir" >> "$profile"
            ;;
    esac

    success "Updated $profile"
    NEED_RELOAD=1
}

# Main installation
main() {
    NEED_RELOAD=0

    printf '%s\n' "${BOLD}fqpack installer${RESET}"
    printf '%s\n' ""

    OS=$(detect_os)
    ARCH=$(detect_arch)
    BINARY="fqpack-${OS}-${ARCH}"

    info "Detected ${OS}/${ARCH}"

    # Get latest version
    RELEASES="https://github.com/vertti/fastqpacker/releases"
    info "Fetching latest version..."
    VERSION=$(download "${RELEASES}/latest" /dev/stdout 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)

    if [ -z "$VERSION" ]; then
        # Fallback: get from redirect
        VERSION=$(curl -fsSL -o /dev/null -w '%{url_effective}' "${RELEASES}/latest" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+')
    fi

    [ -z "$VERSION" ] && error "Could not determine latest version"

    info "Latest version: ${VERSION}"

    URL="${RELEASES}/download/${VERSION}/${BINARY}"

    # Determine install directory
    if [ -w /usr/local/bin ]; then
        INSTALL_DIR="/usr/local/bin"
    elif [ -n "${SUDO_USER:-}" ] || [ "$(id -u)" = "0" ]; then
        INSTALL_DIR="/usr/local/bin"
    else
        INSTALL_DIR="$HOME/.local/bin"
        mkdir -p "$INSTALL_DIR"
    fi

    info "Installing to ${INSTALL_DIR}"

    # Download
    TMP=$(mktemp)
    trap 'rm -f "$TMP"' EXIT

    info "Downloading ${BINARY}..."
    download "$URL" "$TMP" || error "Download failed"

    chmod +x "$TMP"

    # Install (with sudo if needed)
    if [ -w "$INSTALL_DIR" ]; then
        mv "$TMP" "$INSTALL_DIR/fqpack"
    else
        info "Requesting sudo access..."
        sudo mv "$TMP" "$INSTALL_DIR/fqpack"
    fi

    success "Installed fqpack to ${INSTALL_DIR}/fqpack"

    # Add to PATH if needed
    if [ "$INSTALL_DIR" = "$HOME/.local/bin" ]; then
        add_to_path "$INSTALL_DIR"
    fi

    # Verify
    printf '%s\n' ""
    if has fqpack; then
        success "fqpack ${VERSION} is ready!"
        fqpack -version
    elif [ -x "$INSTALL_DIR/fqpack" ]; then
        success "fqpack ${VERSION} installed!"
        "$INSTALL_DIR/fqpack" -version
        if [ "$NEED_RELOAD" = "1" ]; then
            printf '%s\n' ""
            info "Run this to use fqpack now:"
            printf '%s\n' "   ${BOLD}export PATH=\"$INSTALL_DIR:\$PATH\"${RESET}"
            printf '%s\n' ""
            info "Or restart your shell."
        fi
    else
        error "Installation failed"
    fi
}

main
