// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests exercise install.sh deterministically without touching the
// network. Stub curl/wget executables are written into a temp dir and
// prepended to PATH; they emulate the HTTP semantics the installer relies on
// (notably curl -f failing on 4xx/5xx instead of writing the error body).
//
// Regression coverage:
//   - BUG #45 / SCB-047: curl without -f saved HTTP error bodies and the
//     installer treated any non-empty file as a successful download.
//   - BUG #44 / SCB-046: get_latest_version() silently downgraded to a
//     hard-coded v0.3.0 on an "API rate limit exceeded" response.
//   - SCB-042 / GHSA-fj8r-qx6c-m797: checksum verification was a no-op that
//     returned success on mismatch and skipped a missing SHA256SUMS, so a
//     tampered or wrong binary was installed anyway.

// stubCurlScript is a deterministic stand-in for curl. Behavior is selected
// by the STUB_MODE environment variable: success, rate_limit, api_error,
// binary_error, macho_thin, pe, checksum_mismatch or missing_checksums.
// Invocations are appended to the file named by STUB_CURL_LOG when that
// variable is set.
const stubCurlScript = `#!/usr/bin/env bash
# Deterministic stand-in for curl used by install.sh integration tests.
# It emulates the HTTP semantics install.sh relies on and never touches the
# network. Behavior is selected by the STUB_MODE environment variable.
output=""
url=""
fail=0
expect_output=0
for arg in "$@"; do
    if [ "$expect_output" = "1" ]; then
        output="$arg"
        expect_output=0
        continue
    fi
    case "$arg" in
        -o|--output) expect_output=1 ;;
        -*) case "$arg" in *f*) fail=1 ;; esac ;;
        *) url="$arg" ;;
    esac
done

# Record every invocation so tests can assert which flags were actually used.
if [ -n "${STUB_CURL_LOG:-}" ]; then
    printf '%s\n' "$*" >> "$STUB_CURL_LOG"
fi

# sha256 of a file, using whichever tool the host provides (mirrors install.sh).
sha256_of_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

# The release asset name install.sh requests for this platform.
asset_name() {
    local os arch
    case "$(uname -s)" in
        Linux*)   os=linux ;;
        Darwin*)  os=darwin ;;
        MINGW*|MSYS*|CYGWIN*) os=windows ;;
        *)        os=linux ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64)  arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        armv7*)        arch=arm ;;
        *)             arch=amd64 ;;
    esac
    local name="scbake-${os}-${arch}"
    if [ "$os" = "windows" ]; then
        name="${name}.exe"
    fi
    printf '%s' "$name"
}

mode="${STUB_MODE:-success}"

case "$url" in
    *api.github.com*releases/latest*)
        case "$mode" in
            rate_limit) body='{"message":"API rate limit exceeded for 1.2.3.4"}' ;;
            api_error)  body='{"message":"Not Found"}' ;;
            *)          body='{"tag_name":"v9.9.9"}' ;;
        esac
        if [ "$fail" = "1" ] && { [ "$mode" = "rate_limit" ] || [ "$mode" = "api_error" ]; }; then
            # Real curl -f exits non-zero and writes no body on HTTP errors.
            exit 22
        fi
        printf '%s' "$body"
        ;;
    *releases/download*SHA256SUMS*)
        # missing_checksums emulates a release with no SHA256SUMS asset (HTTP
        # 404): curl -f must fail, and the installer must abort.
        if [ "$mode" = "missing_checksums" ]; then
            exit 22
        fi
        # Write the same magic bytes the binary branch serves, hash them, and
        # emit a correctly formatted checksum line for the requested asset.
        tmp=$(mktemp)
        case "$mode" in
            macho_thin) printf '\xcf\xfa\xed\xfe\x02\x01\x01\x00' > "$tmp" ;;
            pe)         printf '\x4d\x5a\x90\x00\x03\x00\x00\x00' > "$tmp" ;;
            *)          printf '\x7fELF\x02\x01\x01\x00' > "$tmp" ;;
        esac
        hash=$(sha256_of_file "$tmp")
        rm -f "$tmp"
        if [ "$mode" = "checksum_mismatch" ]; then
            # A hash that can never match the downloaded binary.
            hash="0000000000000000000000000000000000000000000000000000000000000000"
        fi
        line="$hash  $(asset_name)"
        if [ -n "$output" ]; then
            printf '%s\n' "$line" > "$output"
        else
            printf '%s\n' "$line"
        fi
        ;;
    *releases/download*scbake-*)
        if [ "$mode" = "binary_error" ]; then
            if [ "$fail" = "1" ]; then
                exit 22
            fi
            # HTTP 200 whose body is not a binary (e.g. an error page).
            body='<!DOCTYPE html><html><body>404 Not Found</body></html>'
            if [ -n "$output" ]; then
                printf '%s' "$body" > "$output"
            else
                printf '%s' "$body"
            fi
            exit 0
        fi
        # Select a plausible binary magic for the requested platform. The
        # installer's signature check must accept all of these.
        case "$mode" in
            macho_thin) magic='\xcf\xfa\xed\xfe\x02\x01\x01\x00' ;;  # thin Mach-O (little-endian)
            pe)         magic='\x4d\x5a\x90\x00\x03\x00\x00\x00' ;;  # PE/COFF (Windows)
            *)          magic='\x7fELF\x02\x01\x01\x00' ;;           # ELF (Linux)
        esac
        if [ -n "$output" ]; then
            printf "$magic" > "$output"
        else
            printf "$magic"
        fi
        ;;
    *)
        exit 0
        ;;
esac
`

// stubWgetScript always fails so the installer can never reach the network
// through wget during these tests.
const stubWgetScript = `#!/usr/bin/env bash
# Deterministic stand-in for wget: always fails so tests never hit the network.
exit 8
`

// requireBash skips the test when bash is unavailable (e.g. Windows CI).
func requireBash(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install.sh requires bash; skipping on Windows")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available; skipping install.sh integration tests")
	}
	return bash
}

// installScriptPath resolves the absolute path to install.sh in the repo root.
func installScriptPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "install.sh"))
	if err != nil {
		t.Fatalf("resolve install.sh path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("install.sh not found at %s: %v", path, err)
	}
	return path
}

// setupStubBinaries writes the curl/wget stubs into a temp dir and returns it.
func setupStubBinaries(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"curl": stubCurlScript, "wget": stubWgetScript} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil { //nolint:gosec // test stub
			t.Fatalf("write stub %s: %v", name, err)
		}
	}
	return dir
}

// runInstaller executes install.sh with the stubbed PATH and returns combined
// output plus the error (non-nil means a non-zero exit status).
func runInstaller(t *testing.T, stubDir, mode, installDir string) (string, error) {
	t.Helper()
	out, _, err := runInstallerWithCurlLog(t, stubDir, mode, installDir)
	return out, err
}

// runInstallerWithCurlLog is like runInstaller but additionally returns the
// contents of the stub curl invocation log (one line per invocation). This lets
// a test prove which flags the installer actually passed to curl, rather than
// only observing the downstream behavior.
func runInstallerWithCurlLog(t *testing.T, stubDir, mode, installDir string) (string, string, error) {
	t.Helper()
	bash := requireBash(t)
	script := installScriptPath(t)
	curlLogPath := filepath.Join(t.TempDir(), "curl.log")

	// NOTE: --quiet is intentionally not used. It currently trips a separate,
	// pre-existing set -e issue in the logging helpers (a quiet log_info
	// returns non-zero and aborts the script) that is out of scope here.
	cmd := exec.CommandContext(context.Background(), bash, script, "latest", installDir) //nolint:gosec // test invocation
	env := make([]string, 0, len(os.Environ())+3)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") || strings.HasPrefix(e, "STUB_MODE=") || strings.HasPrefix(e, "QUIET=") || strings.HasPrefix(e, "STUB_CURL_LOG=") {
			continue
		}
		env = append(env, e)
	}
	env = append(env,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STUB_MODE="+mode,
		"STUB_CURL_LOG="+curlLogPath,
	)
	cmd.Env = env

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()

	logData, readErr := os.ReadFile(curlLogPath) //nolint:gosec // test artifact
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read stub curl log: %v", readErr)
	}
	return out.String(), string(logData), err
}

// assertCurlUsedFail verifies that the stub curl recorded at least one
// invocation targeting a URL containing urlSubstr, and that the invocation
// included -f/--fail. It fails loudly when no such invocation was recorded so a
// stub or installer change cannot silently make the assertion vacuous.
func assertCurlUsedFail(t *testing.T, curlLog, urlSubstr string) {
	t.Helper()
	var invocation string
	for _, line := range strings.Split(curlLog, "\n") {
		if strings.Contains(line, urlSubstr) {
			invocation = line
			break
		}
	}
	if invocation == "" {
		t.Fatalf("stub curl recorded no invocation for URL containing %q; log:\n%s", urlSubstr, curlLog)
	}
	if !strings.Contains(invocation, "-f") && !strings.Contains(invocation, "--fail") {
		t.Errorf("curl invocation for %q is missing -f/--fail: %s", urlSubstr, invocation)
	}
}

func installedBinaryPath(installDir string) string {
	return filepath.Join(installDir, "scbake")
}

func assertNotInstalled(t *testing.T, installDir string) {
	t.Helper()
	if _, err := os.Stat(installedBinaryPath(installDir)); !os.IsNotExist(err) {
		t.Errorf("installer wrote a binary despite the failure (stat err: %v)", err)
	}
}

// TestInstallScriptRejectsRateLimit is the primary regression for BUG #44:
// an "API rate limit exceeded" response must fail loudly and must never
// downgrade to the hard-coded v0.3.0.
func TestInstallScriptRejectsRateLimit(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")

	out, err := runInstaller(t, stubDir, "rate_limit", installDir)
	if err == nil {
		t.Fatalf("expected a non-zero exit on a rate-limited API response, got success\noutput:\n%s", out)
	}
	if strings.Contains(out, "v0.3.0") {
		t.Errorf("installer fell back to the hard-coded v0.3.0\noutput:\n%s", out)
	}
	assertNotInstalled(t, installDir)
}

// TestInstallScriptRejectsAPIError ensures a generic API error also fails
// clearly rather than parsing a version out of an error body.
func TestInstallScriptRejectsAPIError(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")

	out, err := runInstaller(t, stubDir, "api_error", installDir)
	if err == nil {
		t.Fatalf("expected a non-zero exit on an API error response, got success\noutput:\n%s", out)
	}
	if strings.Contains(out, "v0.3.0") {
		t.Errorf("installer fell back to the hard-coded v0.3.0\noutput:\n%s", out)
	}
	assertNotInstalled(t, installDir)
}

// TestInstallScriptRejectsErrorBodyDownload is the primary regression for
// BUG #45: when the binary download returns an HTTP error body, curl -f must
// make the download fail instead of installing the error page.
//
// It asserts both the observable outcome (no binary installed) and that the
// download invocation actually carried -f. The latter matters because the
// signature check would also reject the HTML body, so without inspecting the
// invocation the test would pass even if -f were removed.
func TestInstallScriptRejectsErrorBodyDownload(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")

	out, curlLog, err := runInstallerWithCurlLog(t, stubDir, "binary_error", installDir)
	if err == nil {
		t.Fatalf("expected a non-zero exit when the binary download returns an error body, got success\noutput:\n%s", out)
	}
	assertNotInstalled(t, installDir)
	assertCurlUsedFail(t, curlLog, "scbake-")
}

// TestInstallScriptRejectsChecksumMismatch is the primary regression for
// SCB-042: when SHA256SUMS lists a hash that does not match the downloaded
// binary, the installer must abort and must not install anything. Before the
// fix, verify_checksum() returned 0 on mismatch and the binary was installed.
func TestInstallScriptRejectsChecksumMismatch(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")

	out, curlLog, err := runInstallerWithCurlLog(t, stubDir, "checksum_mismatch", installDir)
	if err == nil {
		t.Fatalf("expected a non-zero exit on a checksum mismatch, got success\noutput:\n%s", out)
	}
	assertNotInstalled(t, installDir)
	assertCurlUsedFail(t, curlLog, "SHA256SUMS")
	if !strings.Contains(out, "Checksum mismatch") {
		t.Errorf("expected a checksum mismatch error, got:\n%s", out)
	}
}

// TestInstallScriptRejectsMissingChecksums covers the other half of SCB-042:
// when the release has no SHA256SUMS (the download fails), the installer must
// abort rather than silently skip verification and install the binary.
func TestInstallScriptRejectsMissingChecksums(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")

	out, curlLog, err := runInstallerWithCurlLog(t, stubDir, "missing_checksums", installDir)
	if err == nil {
		t.Fatalf("expected a non-zero exit when SHA256SUMS is unavailable, got success\noutput:\n%s", out)
	}
	assertNotInstalled(t, installDir)
	assertCurlUsedFail(t, curlLog, "SHA256SUMS")
	if !strings.Contains(out, "Failed to download checksums") {
		t.Errorf("expected a fatal checksum-download error, got:\n%s", out)
	}
}

// TestInstallScriptAcceptsThinMachO guards against the regression where the
// signature check only accepted fat Mach-O (cafebabe) and ELF/PE, causing every
// macOS install to abort: GoReleaser emits thin Mach-O binaries whose magic is
// cffaedfe (little-endian) / feedface (big-endian). The installer must accept
// those. The stub serves cffaedfe as the first four bytes.
func TestInstallScriptAcceptsThinMachO(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")

	out, err := runInstaller(t, stubDir, "macho_thin", installDir)
	if err != nil {
		t.Fatalf("expected a thin Mach-O binary to be accepted, got %v\noutput:\n%s", err, out)
	}

	data, readErr := os.ReadFile(installedBinaryPath(installDir))
	if readErr != nil {
		t.Fatalf("installed binary missing: %v", readErr)
	}
	if !bytes.HasPrefix(data, []byte{0xcf, 0xfa, 0xed, 0xfe}) {
		t.Errorf("installed binary has an unexpected magic number: % x", data)
	}
}

// TestInstallScriptAcceptsPE covers the Windows magic (4d5a) so the accepted
// set cannot be narrowed without a test failure.
func TestInstallScriptAcceptsPE(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")

	out, err := runInstaller(t, stubDir, "pe", installDir)
	if err != nil {
		t.Fatalf("expected a PE binary to be accepted, got %v\noutput:\n%s", err, out)
	}

	data, readErr := os.ReadFile(installedBinaryPath(installDir))
	if readErr != nil {
		t.Fatalf("installed binary missing: %v", readErr)
	}
	if !bytes.HasPrefix(data, []byte{0x4d, 0x5a}) {
		t.Errorf("installed binary has an unexpected magic number: % x", data)
	}
}

// TestInstallScriptSucceeds confirms the happy path still works end to end.
func TestInstallScriptSucceeds(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")

	out, curlLog, err := runInstallerWithCurlLog(t, stubDir, "success", installDir)
	if err != nil {
		t.Fatalf("expected a successful install, got %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "Checksum verified") {
		t.Errorf("expected the installer to report a verified checksum, got:\n%s", out)
	}
	assertCurlUsedFail(t, curlLog, "SHA256SUMS")

	data, readErr := os.ReadFile(installedBinaryPath(installDir))
	if readErr != nil {
		t.Fatalf("installed binary missing: %v", readErr)
	}
	if !bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) {
		t.Errorf("installed binary has an unexpected magic number: % x", data)
	}
}

// TestInstallScriptHardeningStatic is a fast, network-free guard that fails if
// the hard-coded downgrade reappears or if any curl invocation drops -f.
func TestInstallScriptHardeningStatic(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "v0.3.0") {
		t.Error("install.sh still contains the hard-coded downgrade version v0.3.0")
	}

	for i, raw := range strings.Split(content, "\n") {
		line := raw
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx] // ignore comments
		}
		if !strings.Contains(line, "curl -") {
			continue
		}
		if !strings.Contains(line, "-f") {
			t.Errorf("install.sh:%d: curl invocation is missing -f: %s", i+1, strings.TrimSpace(raw))
		}
	}
}

// TestInstallScriptChecksumVerificationStatic is a fast, network-free guard that
// fails if checksum verification regresses to a best-effort no-op: a missing
// SHA256SUMS or a mismatch must abort the install, not warn and continue.
func TestInstallScriptChecksumVerificationStatic(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	content := string(data)

	// The old no-op tolerated a missing/mismatched checksum; none of these may
	// reappear.
	for _, forbidden := range []string{
		"--ignore-missing",
		"Checksum validation skipped",
		"Checksums unavailable",
		"Don't die",
	} {
		if strings.Contains(content, forbidden) {
			t.Errorf("install.sh still tolerates a checksum failure (%q present)", forbidden)
		}
	}

	// The fatal paths must exist.
	for _, want := range []string{
		"Checksum mismatch for",
		"No checksum entry for",
		"Failed to download checksums",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("install.sh is missing fatal checksum handling (%q absent)", want)
		}
	}
}
