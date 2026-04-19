# Installing scbake

scbake is a single-binary CLI tool. Choose your preferred installation method below.

## Quick Install (Recommended)

One-liner for macOS, Linux, and Windows (WSL/Git Bash):

```bash
curl -sSL https://raw.githubusercontent.com/Emin-ACIKGOZ/scbake/master/install.sh | bash
```

Or with `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/Emin-ACIKGOZ/scbake/master/install.sh | bash
```

### Installation Options

```bash
# Install latest version to default location
bash install.sh

# Install specific version
bash install.sh v0.0.1

# Install to custom directory
bash install.sh latest /opt/bin

# Quiet mode (no output, suitable for automation/CI)
bash install.sh latest --quiet

# Combined: specific version, custom path, quiet
bash install.sh v0.0.1 ~/.local/bin --quiet
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

## Alternative: Go Install

If you have Go 1.21+ installed:

```bash
go install github.com/Emin-ACIKGOZ/scbake/cmd/scbake@latest
```

Binary will be installed to `$GOPATH/bin/` (usually `~/go/bin/`).

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
- **Checksum verification** - Binaries are verified with SHA256 checksums (automatic if available)
- **Binary validation** - Checks downloaded file is a valid executable (not corrupted or HTML error page)
- **Sudo escalation warnings** - Warns before requesting sudo for elevated installations
- **Retry with backoff** - Failed downloads retry with exponential backoff (1s, 2s, 4s)
- **No sudo for downloads** - `sudo` is only used for final installation to protected directories
- **Minimal dependencies** - Only `curl` or `wget` required; graceful fallbacks throughout

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
