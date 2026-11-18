// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Regression coverage for issue #217: `install.sh latest --quiet` exited 1
// without printing anything and installed nothing. Two defects were involved:
// the log helpers used `[ "$QUIET" -eq 0 ] && echo`, whose false result tripped
// `set -e`, and `--quiet` was left among the positional arguments, so it could
// be assigned to VERSION or INSTALL_DIR.
//
// These tests use the deterministic curl/wget/cosign stubs from
// setupStubBinaries, so they never touch the network.

// runInstallArgs executes install.sh with arbitrary arguments and a controlled
// HOME, PATH and working directory. stdout and stderr are captured separately
// so a quiet-mode test can prove that nothing at all is printed, while a
// failure-path test can prove that errors still reach stderr.
func runInstallArgs(t *testing.T, stubDir, home, workDir, mode string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	bash := requireBash(t)
	script := installScriptPath(t)

	cmdArgs := append([]string{script}, args...)
	cmd := exec.CommandContext(context.Background(), bash, cmdArgs...) //nolint:gosec // test invocation
	cmd.Dir = workDir

	env := make([]string, 0, len(os.Environ())+4)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") ||
			strings.HasPrefix(e, "HOME=") ||
			strings.HasPrefix(e, "QUIET=") ||
			strings.HasPrefix(e, "STUB_MODE=") ||
			strings.HasPrefix(e, "STUB_CURL_LOG=") ||
			strings.HasPrefix(e, "STUB_COSIGN_MODE=") {
			continue
		}
		env = append(env, e)
	}
	env = append(env,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+home,
		"STUB_MODE="+mode,
	)
	cmd.Env = env

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// TestInstallScriptQuietModeWithVersionAndDir covers the documented
// `bash install.sh vX.Y.Z <dir> --quiet` form: it must exit 0, print nothing,
// and install the binary.
func TestInstallScriptQuietModeWithVersionAndDir(t *testing.T) {
	stubDir := setupStubBinaries(t)
	home := t.TempDir()
	workDir := t.TempDir()
	installDir := filepath.Join(t.TempDir(), "bin")

	stdout, stderr, err := runInstallArgs(t, stubDir, home, workDir, "success", "v9.9.9", installDir, "--quiet")
	if err != nil {
		t.Fatalf("quiet install with explicit version/dir failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if combined := stdout + stderr; combined != "" {
		t.Errorf("quiet mode printed output on success:\n%s", combined)
	}
	if _, statErr := os.Stat(installedBinaryPath(installDir)); statErr != nil {
		t.Errorf("binary was not installed to %s: %v", installDir, statErr)
	}
}

// TestInstallScriptQuietModeLatestUsesDefaultDir covers `bash install.sh latest
// --quiet` with HOME pointed at a temp dir. `--quiet` must be consumed as a
// flag, not as the install directory, and the binary must land in the default
// location ($HOME/.local/bin).
func TestInstallScriptQuietModeLatestUsesDefaultDir(t *testing.T) {
	// determine_install_dir prefers /usr/local/bin when it is writable. In that
	// case (e.g. a root container) $HOME/.local/bin is not the documented
	// default for this environment, so this assertion cannot hold.
	bash := requireBash(t)
	if err := exec.CommandContext(context.Background(), bash, "-c", "[ -w /usr/local/bin ]").Run(); err == nil { //nolint:gosec // fixed command, test probe
		t.Skip("host has a writable /usr/local/bin; the default install dir would not be $HOME/.local/bin")
	}

	stubDir := setupStubBinaries(t)
	home := t.TempDir()
	workDir := t.TempDir()

	stdout, stderr, err := runInstallArgs(t, stubDir, home, workDir, "success", "latest", "--quiet")
	if err != nil {
		t.Fatalf("quiet install with `latest --quiet` failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if combined := stdout + stderr; combined != "" {
		t.Errorf("quiet mode printed output on success:\n%s", combined)
	}

	want := filepath.Join(home, ".local", "bin", "scbake")
	if _, statErr := os.Stat(want); statErr != nil {
		t.Errorf("binary was not installed to the default location %s: %v", want, statErr)
	}

	// `--quiet` must never be materialized as an install directory.
	for _, dir := range []string{workDir, home} {
		quietDir := filepath.Join(dir, "--quiet")
		if _, statErr := os.Stat(quietDir); !os.IsNotExist(statErr) {
			t.Errorf("installer created a directory literally named %q (stat err: %v)", quietDir, statErr)
		}
	}
}

// TestInstallScriptQuietModeStillReportsErrors ensures quiet mode only
// suppresses informational output: a fatal checksum mismatch must still exit
// non-zero and write a message to stderr.
func TestInstallScriptQuietModeStillReportsErrors(t *testing.T) {
	stubDir := setupStubBinaries(t)
	home := t.TempDir()
	workDir := t.TempDir()
	installDir := filepath.Join(t.TempDir(), "bin")

	stdout, stderr, err := runInstallArgs(t, stubDir, home, workDir, "checksum_mismatch", "latest", installDir, "--quiet")
	if err == nil {
		t.Fatalf("expected a non-zero exit on a checksum mismatch in quiet mode, got success\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stderr == "" {
		t.Fatal("quiet mode swallowed the fatal error: stderr is empty")
	}
	if !strings.Contains(stderr, "Checksum mismatch") {
		t.Errorf("expected the checksum mismatch error on stderr, got:\n%s", stderr)
	}
	assertNotInstalled(t, installDir)
}
