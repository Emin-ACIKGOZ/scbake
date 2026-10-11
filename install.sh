#!/bin/bash
# scbake installer - Cross-platform, production-grade installation script
# Usage: curl -sSL https://raw.githubusercontent.com/Emin-ACIKGOZ/scbake/master/install.sh | bash
# Or:    bash install.sh [version] [install-dir] [--quiet]
# Examples:
#   bash install.sh                          # Latest version, interactive
#   bash install.sh vX.Y.Z /opt/scbake       # Specific version to custom path
#   bash install.sh latest --quiet           # Latest, automation-friendly (no output)

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

GITHUB_REPO="Emin-ACIKGOZ/scbake"
GITHUB_API_URL="https://api.github.com/repos/${GITHUB_REPO}"
RELEASE_URL="https://github.com/${GITHUB_REPO}/releases/download"

# Parse arguments - simple positional
VERSION="${1:-latest}"
INSTALL_DIR="${2:-}"
QUIET="${QUIET:-0}"

# Check for --quiet flag anywhere in args
for arg in "$@"; do
    [[ "$arg" == "--quiet" || "$arg" == "-q" ]] && QUIET=1
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

# Spinner for long operations
spinner() {
    local pid=$!
    local delay=0.1
    local frames=( '⠋' '⠙' '⠹' '⠸' '⠼' '⠴' '⠦' '⠧' '⠇' '⠏' )

    while kill -0 $pid 2>/dev/null; do
        for frame in "${frames[@]}"; do
            echo -ne "\r${BLUE}${frame}${NC} $*"
            sleep $delay
        done
    done
    echo -ne "\r"
}

# Progress with percentage
progress_step() {
    local step=$1
    local total=$2
    local msg=$3
    local percent=$((step * 100 / total))

    [ "$QUIET" -eq 0 ] && printf "${BLUE}[%3d%%]${NC} %s\n" "$percent" "$msg"
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

# Get latest release version from GitHub API.
# Any API failure (HTTP error, rate limiting, unparseable response) is fatal.
# We never fall back to a hard-coded version: doing so would let an attacker
# (or a transient rate limit) silently downgrade the user to a stale build.
get_latest_version() {
    local url="${GITHUB_API_URL}/releases/latest"
    local version
    local response

    if command_exists curl; then
        # -f makes curl exit non-zero on HTTP 4xx/5xx instead of writing the
        # error body to stdout and reporting success.
        if ! response=$(curl -fsSL "$url" 2>/dev/null); then
            die "Failed to query the GitHub releases API ($url). Check your network connection, or GitHub may be rate limiting requests. Retry later, or install a specific version: bash install.sh <version> <path>"
        fi
    elif command_exists wget; then
        if ! response=$(wget -qO- "$url" 2>/dev/null); then
            die "Failed to query the GitHub releases API ($url). Check your network connection, or GitHub may be rate limiting requests. Retry later, or install a specific version: bash install.sh <version> <path>"
        fi
    else
        die "Neither curl nor wget found. Please install one to download scbake."
    fi

    # Try jq first (most reliable)
    if command_exists jq; then
        version=$(echo "$response" | jq -r '.tag_name' 2>/dev/null || true)
        if [ -n "$version" ] && [ "$version" != "null" ]; then
            echo "$version"
            return
        fi
    fi

    # Fallback to grep for systems without jq
    version=$(echo "$response" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | cut -d'"' -f4 || true)

    if [ -z "$version" ]; then
        # Last resort: try basic grep
        version=$(echo "$response" | grep "tag_name" | head -1 | cut -d'"' -f4 || true)
    fi

    if [ -z "$version" ]; then
        die "Failed to parse the latest version from the GitHub API response. Try installing a specific version: bash install.sh <version> <path>"
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
    local status
    local dl_pid

    for attempt in $(seq 1 $retries); do
        # Discard any stale/partial file so a failed attempt can never be
        # mistaken for a successful one.
        rm -f "$output"
        status=0

        if command_exists curl; then
            # -f makes curl fail on HTTP 4xx/5xx instead of saving the error
            # body and exiting 0.
            if [ "$QUIET" -eq 0 ]; then
                curl -fsSL -o "$output" "$url" 2>/dev/null &
                dl_pid=$!
                spinner "Downloading binary..."
                wait "$dl_pid" || status=$?
            else
                curl -fsSL -o "$output" "$url" 2>/dev/null || status=$?
            fi
        elif command_exists wget; then
            if [ "$QUIET" -eq 0 ]; then
                wget -q -O "$output" "$url" 2>/dev/null &
                dl_pid=$!
                spinner "Downloading binary..."
                wait "$dl_pid" || status=$?
            else
                wget -q -O "$output" "$url" 2>/dev/null || status=$?
            fi
        else
            die "Neither curl nor wget found"
        fi

        if [ "$status" -eq 0 ] && [ -s "$output" ]; then
            return 0
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

# Verify checksum (optional, non-fatal if missing or mismatched)
verify_checksum() {
    local binary="$1"
    local checksums_file="$2"

    if [ ! -f "$checksums_file" ]; then
        return 0  # No checksums file, skip silently
    fi

    # Try sha256sum first (standard on Linux)
    if command_exists sha256sum; then
        if sha256sum -c "$checksums_file" --ignore-missing >/dev/null 2>&1; then
            log_success "Checksum verified"
            return 0
        else
            log_warn "Checksum mismatch (but binary downloaded successfully)"
            return 0  # Don't die, binary might still be valid
        fi
    # Fallback to shasum (macOS, some BSD systems)
    elif command_exists shasum; then
        if grep "$(shasum -a 256 "$binary" | awk '{print $1}')" "$checksums_file" >/dev/null 2>&1; then
            log_success "Checksum verified"
            return 0
        else
            log_warn "Checksum mismatch (but binary downloaded successfully)"
            return 0  # Don't die
        fi
    else
        return 0  # No checksum tool, skip silently
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
    local os_arch="$2"
    local path_sep=":"

    if [[ "$os_arch" == *"windows"* ]]; then
        path_sep=";"
    fi

    if ! echo "${path_sep}${PATH}${path_sep}" | grep -qE "(^|${path_sep})${install_dir}(${path_sep}|$)"; then
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
    log_info "Initializing scbake installer..."
    log_info "Repository: $GITHUB_REPO"

    log_info "Detecting system..."
    local os_arch
    os_arch=$(detect_system)
    log_success "Detected: $os_arch"

    log_info "Determining version..."
    if [ "$VERSION" = "latest" ]; then
        log_info "Fetching latest release..."
        VERSION=$(get_latest_version)
        log_success "Version: $VERSION"
    else
        log_success "Version: $VERSION"
    fi

    log_info "Selecting installation path..."
    INSTALL_DIR=$(determine_install_dir "$INSTALL_DIR")
    log_info "Location: $INSTALL_DIR"

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

    # Download binary (convert os/arch to filename format: os-arch)
    local filename="scbake-${os_arch//\//-}"
    if [[ "$os_arch" == *"windows"* ]]; then
        filename="${filename}.exe"
    fi

    local download_url="${RELEASE_URL}/${VERSION}/${filename}"
    log_info "Downloading binary..."
    log_info "Source: $download_url"

    download_file "$download_url" "$binary_name"
    log_success "Downloaded successfully"

    log_info "Validating binary..."
    if [ ! -s "$binary_name" ]; then
        die "Downloaded file is empty or invalid"
    fi

    # Check for ELF (Linux), Mach-O (macOS), or PE (Windows) magic numbers.
    # An unrecognized magic number means we downloaded something that is not a
    # scbake binary (e.g. an HTML error page served with HTTP 200), so abort.
    # Only warn when the magic cannot be determined at all (xxd/od unavailable).
    local magic
    magic=$(xxd -l 4 -p "$binary_name" 2>/dev/null || od -A n -t x1 -N 4 "$binary_name" 2>/dev/null | tr -d ' \n' || true)
    if [ -z "$magic" ]; then
        log_warn "Could not verify binary signature (xxd/od unavailable)"
    else
        case "$magic" in
            7f454c46*|cffaedfe*|cefaedfe*|feedface*|feedfacf*|cafebabe*|cafebabf*|4d5a*) log_success "Binary signature verified" ;;
            *) die "Downloaded file is not a valid scbake binary (unexpected magic number: $magic)" ;;
        esac
    fi

    log_info "Verifying checksums..."
    local checksums_url="${RELEASE_URL}/${VERSION}/SHA256SUMS"
    if curl -fsSL -o SHA256SUMS "$checksums_url" 2>/dev/null || wget -q -O SHA256SUMS "$checksums_url" 2>/dev/null; then
        if verify_checksum "$binary_name" "SHA256SUMS" 2>/dev/null; then
            log_success "Checksum verified"
        else
            log_warn "Checksum validation skipped"
        fi
    else
        log_warn "Checksums unavailable, skipped validation"
    fi

    # Make executable
    make_executable "$binary_name"

    log_info "Installing to system..."
    mkdir -p "$INSTALL_DIR" || die "Failed to create install directory"

    # Use sudo if necessary, with user warning
    if [[ "$os_arch" == *"windows"* ]]; then
        if [ ! -w "$INSTALL_DIR" ]; then
            die "Installation directory requires write access. Try: bash install.sh latest ~/.local/bin (a directory in your home)"
        fi
    fi

    if [ ! -w "$INSTALL_DIR" ]; then
        log_warn "Directory '$INSTALL_DIR' requires elevated privileges"
        log_info "Password prompt coming up (or use ~/.local/bin to avoid sudo)"

        if ! sudo -v >/dev/null 2>&1; then
            die "sudo authentication failed. Try: bash install.sh latest ~/.local/bin"
        fi

        sudo mv "$binary_name" "$INSTALL_DIR/$binary_name" || die "Failed to install"
    else
        mv "$binary_name" "$INSTALL_DIR/$binary_name" || die "Failed to install"
    fi

    log_success "Installation complete! ✨"

    if [ "$QUIET" -eq 0 ]; then
        echo ""
        # Verify installation
        if command_exists scbake; then
            local installed_version
            installed_version=$(scbake --version 2>/dev/null | head -1 || echo "unknown")
            log_success "scbake ready: $installed_version"
        else
            check_path "$INSTALL_DIR" "$os_arch"
            log_warn "scbake not in PATH yet. Refresh your terminal."
        fi

        echo ""
        log_info "Ready to build projects:"
        echo "  • scbake new my-app --lang go"
        echo "  • scbake new my-api --lang spring"
        echo "  • scbake new my-ui --lang svelte"
        echo ""
        log_info "Learn more: https://github.com/$GITHUB_REPO#readme"
        echo ""
    fi
}

# =============================================================================
# Entry Point
# =============================================================================

main "$@"
