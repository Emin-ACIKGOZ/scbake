# Installing scbake

scbake is a single-binary CLI tool. Choose your preferred installation method below.

## Quick Install (Recommended)

Install from a **pinned release tag** for macOS, Linux, and Windows
(WSL/Git Bash). A release tag is immutable, and the installer is published as a
release asset alongside its checksum, so you can verify what you are about to
run.

`install.sh` is attached to releases starting with the first release that
includes this installer change; earlier tags do not contain the asset and their
`install.sh` URL 404s. Use the **latest** tag from
<https://github.com/Emin-ACIKGOZ/scbake/releases/latest>:

```bash
# Set TAG to a release that ships install.sh. Use the latest release tag
# (see https://github.com/Emin-ACIKGOZ/scbake/releases/latest) — older tags
# do not contain the installer asset.
TAG=<tag>
BASE="https://github.com/Emin-ACIKGOZ/scbake/releases/download/${TAG}"

# Download the installer and verify it against the release checksums
# (on macOS, use: shasum -a 256 -c -)
curl -fsSL -o install.sh "${BASE}/install.sh"
curl -fsSL -o SHA256SUMS "${BASE}/SHA256SUMS"
grep 'install.sh$' SHA256SUMS | sha256sum -c -

# Run the installer, pinned to the same tag
bash install.sh "${TAG}"
```

> **Avoid piping a branch URL into `bash`.** The old one-liner
> (`curl -sSL .../master/install.sh | bash`) fetches a mutable branch, so the
> script can change between review and execution. Use a pinned release above.

### Installation Options

```bash
# Install the latest version to the default location
bash install.sh

# Install a specific version (tag)
bash install.sh vX.Y.Z

# Install to custom directory
bash install.sh latest /opt/bin

# Quiet mode (no output, suitable for automation/CI)
bash install.sh latest --quiet

# Combined: specific version, custom path, quiet
bash install.sh vX.Y.Z ~/.local/bin --quiet
```

The installer will:
- ✅ Detect your OS and architecture (Linux, macOS, Windows × amd64, arm64)
- ✅ Download the correct binary from GitHub Releases
- ✅ Verify checksum for security
- ✅ Install to `/usr/local/bin`, `~/.local/bin`, or your custom path
- ✅ Make it executable and ready to use
- ✅ Show next steps for your shell

### Supported Platforms

| OS | Architecture | Status |
|---|---|---|
| Linux | x86_64, ARM64 | ✅ Tested |
| macOS | x86_64, ARM64 (Apple Silicon) | ✅ Tested |
| Windows | x86_64, ARM64 (WSL/Git Bash) | ✅ Tested |

## Verifying a release

Every release publishes, alongside the binaries and `SHA256SUMS`:

- `SHA256SUMS.sigstore.json` — a keyless [Sigstore](https://www.sigstore.dev/)
  signature over `SHA256SUMS`, produced by cosign in the release workflow.
- `scbake_<version>_<os>_<arch>.sbom.json` — an SPDX SBOM (generated with Syft).
- SLSA build provenance attestations (verified with `gh`).

`install.sh` verifies the signature automatically when
[cosign](https://docs.sigstore.dev/cosign/system_config/installation/) is on your
`PATH`; set `SCBAKE_REQUIRE_SIGNATURE=1` to make a release without a verifiable
signature abort the install.

To verify by hand (cosign and GNU `sha256sum`/`shasum`):

```bash
TAG=<tag>   # e.g. v0.5.22
BASE="https://github.com/Emin-ACIKGOZ/scbake/releases/download/${TAG}"

curl -fsSL -O "${BASE}/SHA256SUMS"
curl -fsSL -O "${BASE}/SHA256SUMS.sigstore.json"

# 1. Verify the signature over SHA256SUMS against the release workflow identity.
#    The signature is created by the workflow run on refs/heads/master.
cosign verify-blob \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity 'https://github.com/Emin-ACIKGOZ/scbake/.github/workflows/release.yml@refs/heads/master' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  SHA256SUMS

# 2. Only then check the artifact hash.
curl -fsSL -O "${BASE}/scbake-linux-amd64"
sha256sum --check --ignore-missing SHA256SUMS
```

Verify build provenance with the GitHub CLI:

```bash
gh attestation verify scbake-linux-amd64 --repo Emin-ACIKGOZ/scbake
```

## Alternative: Go Install (from a local clone)

The module path is `scbake`, so remote `go install` commands such as
`go install github.com/Emin-ACIKGOZ/scbake@latest` fail with a module-path
mismatch. Clone and install locally instead:

```bash
git clone https://github.com/Emin-ACIKGOZ/scbake.git
cd scbake
go install .
```

The binary is installed to `$(go env GOBIN)`, or `$(go env GOPATH)/bin`
(usually `~/go/bin/`).

## Manual Installation

Download the binary for your platform from [GitHub Releases](https://github.com/Emin-ACIKGOZ/scbake/releases):

1. Visit the [releases page](https://github.com/Emin-ACIKGOZ/scbake/releases)
2. Download `scbake-<os>-<arch>` for your platform
3. Make it executable: `chmod +x scbake-linux-amd64`
4. Move to your PATH: `mv scbake-linux-amd64 /usr/local/bin/scbake`

## Verification

After installation, verify it works:

```bash
scbake --version    # Show version
scbake --help       # Show available commands
```

## Troubleshooting

### Command not found: `scbake`

The install directory is not in your `PATH`. The installer will suggest how to add it:

```bash
# Option 1: Add to current session
export PATH="/usr/local/bin:$PATH"

# Option 2: Add permanently to ~/.bashrc or ~/.zshrc
echo 'export PATH="/usr/local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

### Installation failed with permission error

The installer will try to use `sudo` for protected directories. If it still fails:

```bash
# Install to home directory instead
bash install.sh latest ~/.local/bin
```

### "Neither curl nor wget found"

Install one of them:

```bash
# macOS
brew install curl

# Ubuntu/Debian
sudo apt-get install curl

# Fedora/RHEL
sudo yum install curl
```

### Network issues or GitHub API rate limit

If the installer can't reach GitHub, manually download the binary:

1. Visit https://github.com/Emin-ACIKGOZ/scbake/releases/latest
2. Download the binary for your OS/arch
3. Move to `/usr/local/bin/` and make executable

## Updating scbake

To update to the latest version:

```bash
bash install.sh latest
```

Or reinstall using the quick install command at the top of this page.

## Security

- **HTTPS-only downloads** - All downloads use secure HTTPS connections
- **Mandatory checksum verification** - The installer downloads the release's `SHA256SUMS` and verifies the binary against it; installation aborts if the checksums file is missing or the hash does not match
- **Signed checksums** - The release workflow signs `SHA256SUMS` keylessly with cosign (Sigstore); when cosign is available the installer verifies the signature and pins it to this repository's release workflow identity and the GitHub Actions OIDC issuer. Set `SCBAKE_REQUIRE_SIGNATURE=1` to make verification mandatory
- **SBOM and provenance** - Each release ships an SPDX SBOM and SLSA build provenance you can verify with `gh attestation verify`
- **Binary validation** - Checks downloaded file is a valid executable (not corrupted or HTML error page)
- **Sudo escalation warnings** - Warns before requesting sudo for elevated installations
- **Retry with backoff** - Failed downloads retry with exponential backoff (1s, 2s, 4s)
- **No sudo for downloads** - `sudo` is only used for final installation to protected directories
- **Minimal dependencies** - `curl` or `wget`, plus a SHA-256 tool (`sha256sum` or `shasum`); graceful fallbacks throughout

## Next Steps

After installation:

```bash
# View available languages and templates
scbake list

# Create your first project
scbake new my-app --lang go

# Learn more
scbake --help
```

For more information, see:
- [Quick Start Guide](QUICK_START.md)
- [Full Documentation](../README.md)
