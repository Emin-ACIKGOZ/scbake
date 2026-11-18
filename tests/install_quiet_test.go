// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"errors"
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
	// determine_install_dir prefers /usr/local/bin whenever it is writable, and
	// that probe is a hard-coded absolute path, so neither HOME nor PATH can
	// redirect it. The writable case cannot be simulated safely from this test:
	// chmod'ing the host's /usr/local/bin is unacceptable, and it would not help
	// anyway because access(2)/`[ -w ]` reports success for a privileged (root)
	// process regardless of the directory mode; hiding or remounting the path
	// would need a private mount namespace, which is Linux-only, often disabled
	// for unprivileged processes, and unavailable on the macOS/Windows CI
	// runners. Asserting the writable branch would therefore require writing a
	// binary into /usr/local/bin, which this suite must never do.
	//
	// So skip there instead, and note the residual coverage: the non-writable
	// branch (a normal dev box and the ubuntu-latest CI runner) is exercised
	// here; the explicit-directory form is covered by
	// TestInstallScriptQuietModeWithVersionAndDir; and the flag-versus-positional
	// parsing by the plain/quiet table in
	// TestInstallScriptRejectsExtraPositionalArgs.
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
}

// TestInstallScriptRejectsExtraPositionalArgs covers the argument limit: at most
// a version and an install directory are accepted, so a third positional
// argument must fail loudly with exit status 1 (die) before anything is
// installed. Asserting the exact status, rather than merely non-zero, keeps the
// contract precise: a bash-level failure (2 for a syntax error, 127 for a
// missing command) or death by signal (ExitCode -1) would otherwise pass.
func TestInstallScriptRejectsExtraPositionalArgs(t *testing.T) {
	requireBash(t) // skip Windows before doing any work; runInstallArgs re-resolves bash

	stubDir := setupStubBinaries(t)
	home := t.TempDir()
	workDir := t.TempDir()

	for _, tc := range []struct {
		name  string
		quiet bool
	}{
		{name: "plain"},
		{name: "quiet", quiet: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installDir := filepath.Join(t.TempDir(), "bin")
			args := []string{"v9.9.9", installDir, "extra"}
			if tc.quiet {
				args = append(args, "--quiet")
			}

			stdout, stderr, err := runInstallArgs(t, stubDir, home, workDir, "success", args...)
			if err == nil {
				t.Fatalf("expected a non-zero exit for extra positional arguments\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected the installer to exit with a status, got %T: %v\nstdout:\n%s\nstderr:\n%s", err, err, stdout, stderr)
			}
			if code := exitErr.ExitCode(); code != 1 {
				t.Errorf("expected exit code 1 (die) for extra positional arguments, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			if !strings.Contains(stderr, "Too many arguments") {
				t.Errorf("expected a 'Too many arguments' error on stderr, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			assertNotInstalled(t, installDir)
		})
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
