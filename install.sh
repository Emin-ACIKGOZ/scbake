#!/bin/bash
# scbake installer - Cross-platform, production-grade installation script
# Usage: curl -fsSL https://github.com/Emin-ACIKGOZ/scbake/releases/download/<tag>/install.sh | bash
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
    local label="${3:-Downloading binary...}"
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
                spinner "$label"
                wait "$dl_pid" || status=$?
            else
                curl -fsSL -o "$output" "$url" 2>/dev/null || status=$?
            fi
        elif command_exists wget; then
            if [ "$QUIET" -eq 0 ]; then
                wget -q -O "$output" "$url" 2>/dev/null &
                dl_pid=$!
                spinner "$label"
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

    # Return non-zero instead of dying so the caller can add context (binary
    # vs. checksums) while still treating exhaustion as fatal.
    return 1
}

# Verify the downloaded binary against the release's SHA256SUMS. This check is
# mandatory and fatal: a missing checksums file, a missing entry for the asset,
# no hashing tool, or a hash mismatch all abort the install. A tampered or wrong
# binary must never be installed.
verify_checksum() {
    local binary="$1"          # local path to the downloaded binary
    local checksums_file="$2"  # path to the SHA256SUMS file
    local asset_name="$3"      # release asset filename to look up
    local expected
    local actual

    if [ ! -f "$checksums_file" ]; then
        die "Checksum file not found: $checksums_file"
    fi

    # Look up the line for exactly this asset (field 2) instead of checking
    # every entry in the file: a checksums file that simply lacks this asset
    # must not "pass". Some tools prefix the filename with '*' (binary mode), so
    # strip that before comparing.
    expected=$(awk -v name="$asset_name" \
        '{ n = $2; sub(/^\*/, "", n); if (n == name) { print $1; exit } }' \
        "$checksums_file" 2>/dev/null || true)

    if [ -z "$expected" ]; then
        die "No checksum entry for '$asset_name' in $checksums_file"
    fi

    if command_exists sha256sum; then
        actual=$(sha256sum "$binary" | awk '{print $1}')
    elif command_exists shasum; then
        actual=$(shasum -a 256 "$binary" | awk '{print $1}')
    else
        die "Neither sha256sum nor shasum found; cannot verify the download"
    fi

    if [ "$actual" != "$expected" ]; then
        die "Checksum mismatch for '$asset_name': expected $expected, got $actual"
    fi

    log_success "Checksum verified"
}

# Verify the SHA256SUMS signature with cosign when available.
#
# SHA256SUMS is signed keylessly by the release workflow (Sigstore), so a valid
# signature is cryptographically bound to that workflow's OIDC identity and
# cannot be forged by whoever can merely serve files from the release. cosign is
# not installed everywhere, so when it is absent we warn and continue by
# default; set SCBAKE_REQUIRE_SIGNATURE=1 to make an unverifiable release fatal.
verify_release_signature() {
    local version="$1"

    local bundle="SHA256SUMS.sigstore.json"
    local bundle_url="${RELEASE_URL}/${version}/${bundle}"
    local identity="https://github.com/${GITHUB_REPO}/.github/workflows/release.yml@refs/heads/master"
    local issuer="https://token.actions.githubusercontent.com"
    local manual="cosign verify-blob --bundle ${bundle} --certificate-identity '${identity}' --certificate-oidc-issuer '${issuer}' SHA256SUMS"

    if ! command_exists cosign; then
        log_warn "cosign is not installed; skipping signature verification"
        log_info "After installing cosign (https://docs.sigstore.dev/cosign/system_config/installation/), verify with:"
        log_info "  $manual"
        if [ "${SCBAKE_REQUIRE_SIGNATURE:-0}" = "1" ]; then
            die "SCBAKE_REQUIRE_SIGNATURE=1 but cosign is not installed"
        fi
        return 0
    fi

    # Releases published before signing was introduced have no bundle. Treat a
    # missing bundle as "unsigned", not as a verification failure, so a pinned
    # install of an older tag keeps working.
    if ! download_file "$bundle_url" "$bundle" "Downloading signature..."; then
        log_warn "No signature bundle at $bundle_url (unsigned release or pre-signing tag)"
        if [ "${SCBAKE_REQUIRE_SIGNATURE:-0}" = "1" ]; then
            die "SCBAKE_REQUIRE_SIGNATURE=1 but $bundle could not be downloaded"
        fi
        return 0
    fi

    if ! cosign verify-blob \
        --bundle "$bundle" \
        --certificate-identity "$identity" \
        --certificate-oidc-issuer "$issuer" \
        "SHA256SUMS" >/dev/null 2>&1; then
        die "Signature verification failed for SHA256SUMS (cosign verify-blob rejected the bundle)"
    fi

    log_success "Signature verified"
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

    download_file "$download_url" "$binary_name" || die "Failed to download binary from $download_url"
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
            7f454c46*|cffaedfe*|cefaedfe*|feedface*|feedfacf*|cafebabe*|cafebabf*|bebafeca*|bfbafeca*|4d5a*) log_success "Binary signature verified" ;;
            *) die "Downloaded file is not a valid scbake binary (unexpected magic number: $magic)" ;;
        esac
    fi

    # Download the checksum manifest. Unlike before, this is not best-effort:
    # if it cannot be fetched we abort rather than install an unverified binary.
    # Route it through download_file so a transient network blip gets the same
    # retries + exponential backoff as the binary; exhaustion is still fatal.
    log_info "Verifying checksums..."
    local checksums_url="${RELEASE_URL}/${VERSION}/SHA256SUMS"
    download_file "$checksums_url" "SHA256SUMS" || die "Failed to download checksums from $checksums_url"

    if [ ! -s SHA256SUMS ]; then
        die "Downloaded checksum file is empty: $checksums_url"
    fi

    verify_checksum "$binary_name" "SHA256SUMS" "$filename"

    log_info "Verifying signature..."
    verify_release_signature "$VERSION"

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
