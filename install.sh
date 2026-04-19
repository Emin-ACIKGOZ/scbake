#!/bin/bash
# scbake installer - Cross-platform, production-grade installation script
# Usage: curl -sSL https://raw.githubusercontent.com/Emin-ACIKGOZ/scbake/master/install.sh | bash
# Or:    bash install.sh [version] [install-dir] [--quiet]
# Examples:
#   bash install.sh                          # Latest version, interactive
#   bash install.sh v0.0.1 /opt/scbake       # Specific version to custom path
#   bash install.sh latest --quiet           # Latest, automation-friendly (no output)

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

GITHUB_REPO="Emin-ACIKGOZ/scbake"
GITHUB_API_URL="https://api.github.com/repos/${GITHUB_REPO}"
RELEASE_URL="https://github.com/${GITHUB_REPO}/releases/download"

# Default values
VERSION="${1:-latest}"
INSTALL_DIR="${2:-}"
QUIET="${QUIET:-0}"

# Parse flags
while [[ $# -gt 0 ]]; do
    case "$1" in
        --quiet|-q) QUIET=1; shift ;;
        latest|v*) VERSION="$1"; shift ;;
        /*) INSTALL_DIR="$1"; shift ;;
        *) shift ;;
    esac
done

# Color codes for output (disabled in quiet mode)
if [ "$QUIET" -eq 1 ]; then
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
else
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
fi
NC='\033[0m' # No Color

# =============================================================================
# Helper Functions
# =============================================================================

log_info() {
    [ "$QUIET" -eq 0 ] && echo -e "${BLUE}ℹ${NC} $*"
}

log_success() {
    [ "$QUIET" -eq 0 ] && echo -e "${GREEN}✓${NC} $*"
}

log_warn() {
    [ "$QUIET" -eq 0 ] && echo -e "${YELLOW}⚠${NC} $*"
}

log_error() {
    echo -e "${RED}✗${NC} $*" >&2  # Always show errors
}

die() {
    log_error "$*"
    exit 1
}

# Detect OS and architecture
detect_system() {
    local os arch

    case "$(uname -s)" in
        Linux*)     os="linux" ;;
        Darwin*)    os="darwin" ;;
        MINGW*|MSYS*|CYGWIN*) os="windows" ;;
        *)          die "Unsupported operating system: $(uname -s)" ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)   arch="amd64" ;;
        aarch64|arm64)  arch="arm64" ;;
        armv7*)         arch="arm" ;;
        *)              die "Unsupported architecture: $(uname -m)" ;;
    esac

    echo "${os}/${arch}"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Get latest release version from GitHub API
get_latest_version() {
    local url="${GITHUB_API_URL}/releases/latest"
    local version

    if command_exists curl; then
        version=$(curl -sSL "$url" | grep -o '"tag_name":"[^"]*"' | cut -d'"' -f4)
    elif command_exists wget; then
        version=$(wget -qO- "$url" | grep -o '"tag_name":"[^"]*"' | cut -d'"' -f4)
    else
        die "Neither curl nor wget found. Please install one to download scbake."
    fi

    if [ -z "$version" ]; then
        die "Failed to fetch latest version from GitHub"
    fi

    echo "$version"
}

# Download file with curl or wget (with retry logic and exponential backoff)
download_file() {
    local url="$1"
    local output="$2"
    local retries=3
    local delay=1
    local attempt

    for attempt in $(seq 1 $retries); do
        if command_exists curl; then
            if curl -sSL -o "$output" "$url" 2>/dev/null; then
                return 0
            fi
        elif command_exists wget; then
            if wget -q -O "$output" "$url" 2>/dev/null; then
                return 0
            fi
        else
            die "Neither curl nor wget found"
        fi

        # Skip sleep on final attempt (no point waiting before exit)
        if [ $attempt -lt $retries ]; then
            log_warn "Download failed, retrying in ${delay}s... (attempt $attempt/$retries)"
            sleep "$delay"
            delay=$((delay * 2))  # Exponential backoff: 1s, 2s, 4s
        fi
    done

    die "Failed to download after $retries attempts: $url"
}

# Verify checksum (optional, if checksums file exists)
# Handles both sha256sum (Linux) and shasum (macOS) with cross-platform compatibility
verify_checksum() {
    local binary="$1"
    local checksums_file="$2"

    if [ ! -f "$checksums_file" ]; then
        log_warn "No checksums file found, skipping verification"
        return 0
    fi

    # Try sha256sum first (standard on Linux)
    if command_exists sha256sum; then
        if sha256sum -c "$checksums_file" --ignore-missing >/dev/null 2>&1; then
            log_success "Checksum verified"
            return 0
        else
            die "Checksum verification failed"
        fi
    # Fallback to shasum (macOS, some BSD systems)
    elif command_exists shasum; then
        # macOS shasum may not support --ignore-missing; filter file instead
        if grep "$(shasum -a 256 "$binary" | awk '{print $1}')" "$checksums_file" >/dev/null 2>&1; then
            log_success "Checksum verified"
            return 0
        else
            die "Checksum verification failed"
        fi
    else
        log_warn "No checksum tool found, skipping verification"
        return 0
    fi
}

# Determine install directory
determine_install_dir() {
    local dir="$1"

    if [ -n "$dir" ]; then
        echo "$dir"
        return
    fi

    # Priority order: /usr/local/bin, ~/.local/bin, /usr/bin
    if [ -w /usr/local/bin ]; then
        echo /usr/local/bin
    elif [ -w "${HOME}/.local/bin" ] || mkdir -p "${HOME}/.local/bin" 2>/dev/null; then
        echo "${HOME}/.local/bin"
    elif [ -w /usr/bin ]; then
        echo /usr/bin
    else
        die "No writable directory found for installation. Specify with: bash install.sh latest /custom/path"
    fi
}

# Make binary executable (handles Windows)
make_executable() {
    local binary="$1"

    if [ -f "$binary" ]; then
        chmod +x "$binary" || die "Failed to make binary executable"
    fi
}

# Check if PATH includes install directory
check_path() {
    local install_dir="$1"

    if ! echo ":$PATH:" | grep -q ":$install_dir:"; then
        log_warn "Install directory '$install_dir' is not in your PATH"
        log_info "Add it with: export PATH=\"$install_dir:\$PATH\""

        # Suggest adding to shell profile
        if [ -f ~/.bashrc ]; then
            log_info "Or add to ~/.bashrc: echo 'export PATH=\"$install_dir:\$PATH\"' >> ~/.bashrc"
        fi
        if [ -f ~/.zshrc ]; then
            log_info "Or add to ~/.zshrc: echo 'export PATH=\"$install_dir:\$PATH\"' >> ~/.zshrc"
        fi
    fi
}

# =============================================================================
# Main Installation
# =============================================================================

main() {
    log_info "scbake installer"
    log_info "Repository: $GITHUB_REPO"
    echo ""

    # Detect system
    log_info "Detecting system..."
    local os_arch
    os_arch=$(detect_system)
    log_success "Detected: $os_arch"

    # Parse version
    if [ "$VERSION" = "latest" ]; then
        log_info "Fetching latest version..."
        VERSION=$(get_latest_version)
        log_success "Latest version: $VERSION"
    else
        log_info "Installing version: $VERSION"
    fi
    echo ""

    # Determine install directory
    INSTALL_DIR=$(determine_install_dir "$INSTALL_DIR")
    log_info "Install directory: $INSTALL_DIR"

    # Create temporary directory
    local tmpdir
    tmpdir=$(mktemp -d) || die "Failed to create temporary directory"
    trap "rm -rf '$tmpdir'" EXIT

    cd "$tmpdir"

    # Determine binary name
    local binary_name="scbake"
    if [[ "$os_arch" == *"windows"* ]]; then
        binary_name="scbake.exe"
    fi

    # Download binary
    local filename="scbake-${os_arch}"
    if [[ "$os_arch" == *"windows"* ]]; then
        filename="${filename}.exe"
    fi

    local download_url="${RELEASE_URL}/${VERSION}/${filename}"
    log_info "Downloading from: $download_url"

    download_file "$download_url" "$binary_name"
    log_success "Downloaded"

    # Validate binary is not empty and likely executable (check magic number)
    if [ ! -s "$binary_name" ]; then
        die "Downloaded file is empty or invalid"
    fi

    # Check for ELF (Linux), Mach-O (macOS), or PE (Windows) magic numbers
    local magic
    magic=$(xxd -l 4 -p "$binary_name" 2>/dev/null || od -A n -t x1 -N 4 "$binary_name" 2>/dev/null | tr -d ' \n')
    case "$magic" in
        7f454c46*|fecf*|cafebabe*|4d5a*) ;;  # ELF, Mach-O, PE headers
        *) log_warn "Warning: Downloaded file may not be a valid binary" ;;
    esac

    # Download and verify checksum
    local checksums_url="${RELEASE_URL}/${VERSION}/SHA256SUMS"
    if download_file "$checksums_url" "SHA256SUMS"; then
        verify_checksum "$binary_name" "SHA256SUMS"
    else
        log_warn "Could not download checksums for verification, proceeding without"
    fi

    # Make executable
    make_executable "$binary_name"

    # Install binary
    log_info "Installing to: $INSTALL_DIR/$binary_name"
    mkdir -p "$INSTALL_DIR" || die "Failed to create install directory"

    # Use sudo if necessary, with user warning
    if [ ! -w "$INSTALL_DIR" ]; then
        log_warn "Directory '$INSTALL_DIR' requires elevated privileges (sudo)"
        log_info "This will prompt for your password. Alternatively, install to ~/.local/bin"

        if ! sudo -v >/dev/null 2>&1; then
            die "sudo authentication failed. Try: bash install.sh latest ~/.local/bin"
        fi

        sudo mv "$binary_name" "$INSTALL_DIR/$binary_name" || die "Failed to install"
    else
        mv "$binary_name" "$INSTALL_DIR/$binary_name" || die "Failed to install"
    fi

    log_success "Installation complete!"

    if [ "$QUIET" -eq 0 ]; then
        echo ""
        # Verify installation
        if command_exists scbake; then
            local installed_version
            installed_version=$(scbake --version 2>/dev/null | head -1 || echo "unknown")
            log_success "scbake is ready: $installed_version"
        else
            check_path "$INSTALL_DIR"
            log_warn "scbake not found in PATH. Please verify installation."
        fi

        echo ""
        log_info "Next steps:"
        echo "  1. Run: scbake --help"
        echo "  2. Try: scbake new my-app --lang go"
        echo "  3. Docs: https://github.com/$GITHUB_REPO#readme"
        echo ""
    fi
}

# =============================================================================
# Entry Point
# =============================================================================

main "$@"
