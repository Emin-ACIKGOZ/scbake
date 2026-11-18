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

// stubSudoScript is a deterministic stand-in for sudo. It records every
// invocation, accepts `sudo -v`, and emulates privilege only where install.sh
// needs it: `mkdir -p` under a parent the test made read-only. `mv` is executed
// as-is because the destination directory is owned by the test user once the
// stub has created it.
const stubSudoScript = `#!/usr/bin/env bash
if [ -n "${STUB_SUDO_LOG:-}" ]; then
    printf '%s\n' "$*" >> "$STUB_SUDO_LOG"
fi

if [ "$#" -eq 0 ]; then
    exit 1
fi

if [ "$1" = "-v" ]; then
    exit 0
fi

if [ "$1" = "mkdir" ]; then
    shift
    dir=""
    for arg in "$@"; do
        case "$arg" in
            -*) ;;
            *) dir="$arg" ;;
        esac
    done
    if [ -z "$dir" ]; then
        exit 1
    fi
    parent=$(dirname "$dir")
    if [ -d "$parent" ]; then
        original=$(stat -c '%a' "$parent" 2>/dev/null || stat -f '%Lp' "$parent")
        chmod u+w "$parent" || exit 1
        mkdir -p "$dir"
        status=$?
        chmod "$original" "$parent" || exit 1
        exit "$status"
    fi
    mkdir -p "$dir"
    exit $?
fi

if [ "$1" = "mv" ]; then
    shift
    exec mv "$@"
fi

exit 1
`

// runInstallerPinned runs install.sh for an explicit version (no GitHub API
// call) with the stubbed PATH and records sudo invocations.
func runInstallerPinned(t *testing.T, stubDir, version, installDir, sudoLog string) (string, error) {
	t.Helper()
	bash := requireBash(t)
	script := installScriptPath(t)

	cmd := exec.CommandContext(context.Background(), bash, script, version, installDir) //nolint:gosec // test invocation
	env := make([]string, 0, len(os.Environ())+3)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") ||
			strings.HasPrefix(e, "QUIET=") ||
			strings.HasPrefix(e, "STUB_MODE=") ||
			strings.HasPrefix(e, "STUB_CURL_LOG=") ||
			strings.HasPrefix(e, "STUB_SUDO_LOG=") {
			continue
		}
		env = append(env, e)
	}
	env = append(env,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STUB_MODE=success",
		"STUB_SUDO_LOG="+sudoLog,
	)
	cmd.Env = env

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	return out.String(), err
}

// installStubSudo writes the stub sudo into the stub bin dir so install.sh's
// privilege escalation is observable instead of touching the host.
func installStubSudo(t *testing.T, stubDir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(stubDir, "sudo"), []byte(stubSudoScript), 0o755); err != nil { //nolint:gosec // test stub
		t.Fatalf("write stub sudo: %v", err)
	}
}

// makeReadOnlyParent creates and protects root/opt so unprivileged writes fail,
// and restores the mode afterwards so t.TempDir can clean up.
func makeReadOnlyParent(t *testing.T, root string) string {
	t.Helper()
	parent := filepath.Join(root, "opt")
	if err := os.Mkdir(parent, 0o750); err != nil {
		t.Fatalf("create protected parent: %v", err)
	}
	if err := os.Chmod(parent, 0o555); err != nil { //nolint:gosec // emulate a root-owned parent
		t.Fatalf("protect parent: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(parent, 0o755); err != nil { //nolint:gosec // allow TempDir cleanup
			t.Errorf("restore parent permissions: %v", err)
		}
	})
	return parent
}

// assertSudoLogged checks the stub sudo log records each expected invocation.
func assertSudoLogged(t *testing.T, sudoLog string, wants ...string) {
	t.Helper()
	logData, err := os.ReadFile(sudoLog) //nolint:gosec // test artifact
	if err != nil {
		t.Fatalf("read stub sudo log: %v", err)
	}
	for _, want := range wants {
		if !strings.Contains(string(logData), want) {
			t.Errorf("stub sudo log is missing a %q invocation; log:\n%s", want, logData)
		}
	}
}

// assertSudoNotInvoked fails when the stub sudo left a log, meaning install.sh
// offered or attempted a privileged operation.
func assertSudoNotInvoked(t *testing.T, sudoLog string) {
	t.Helper()
	if logData, err := os.ReadFile(sudoLog); err == nil { //nolint:gosec // test artifact
		t.Errorf("stub sudo was invoked unexpectedly; log:\n%s", logData)
	} else if !os.IsNotExist(err) {
		t.Fatalf("read stub sudo log: %v", err)
	}
}

// requireExitCode asserts err is a process exit with the wanted status.
func requireExitCode(t *testing.T, err error, out string, want int) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected exit code %d, got %v\noutput:\n%s", want, err, out)
	}
	if code := exitErr.ExitCode(); code != want {
		t.Fatalf("expected exit code %d, got %d\noutput:\n%s", want, code, out)
	}
}

// TestInstallScriptProtectedNewPath is the regression test for #216: a new
// install directory under a protected parent (e.g. /opt/scbake) must be created
// with sudo instead of dying at the unprivileged mkdir. The stub sudo proves
// both the directory creation and the move were escalated.
func TestInstallScriptProtectedNewPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0555 does not block writes, so no sudo would be needed")
	}
	stubDir := setupStubBinaries(t)
	installStubSudo(t, stubDir)

	root := t.TempDir()
	parent := makeReadOnlyParent(t, root)
	installDir := filepath.Join(parent, "scbake-bin")
	sudoLog := filepath.Join(root, "sudo.log")

	out, err := runInstallerPinned(t, stubDir, "v9.9.9", installDir, sudoLog)
	if err != nil {
		t.Fatalf("expected a protected new path to install via sudo, got %v\noutput:\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "scbake")); statErr != nil {
		t.Fatalf("installed binary missing: %v", statErr)
	}

	assertSudoLogged(t, sudoLog, "mkdir -p", "mv")
}

// TestInstallScriptMkdirFailureNoSudo covers a mkdir failure that has nothing to
// do with privileges: the install path already exists as a regular file under a
// writable parent. install.sh must fail with a clear message instead of
// prompting for sudo, which could never fix the problem.
func TestInstallScriptMkdirFailureNoSudo(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installStubSudo(t, stubDir)

	root := t.TempDir()
	parent := filepath.Join(root, "bindir")
	if err := os.Mkdir(parent, 0o750); err != nil {
		t.Fatalf("create writable parent: %v", err)
	}

	// A regular file where the installer wants its directory.
	installDir := filepath.Join(parent, "scbake-bin")
	if err := os.WriteFile(installDir, []byte("not a directory"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("create blocking file: %v", err)
	}

	sudoLog := filepath.Join(root, "sudo.log")
	out, err := runInstallerPinned(t, stubDir, "v9.9.9", installDir, sudoLog)
	requireExitCode(t, err, out, 1)
	if !strings.Contains(out, "Failed to create install directory '"+installDir+"'") {
		t.Errorf("expected a clear mkdir failure message naming the path, got:\n%s", out)
	}
	// The captured mkdir diagnostic must survive, not be swallowed by 2>/dev/null.
	// GNU mkdir says "File exists" on some platforms and "Already exists" on
	// others; require the captured diagnostic, not one exact translation.
	if !strings.Contains(strings.ToLower(out), "exist") || !strings.Contains(out, "mkdir:") {
		t.Errorf("expected the underlying mkdir diagnostic, got:\n%s", out)
	}
	if strings.Contains(out, "requires elevated privileges") || strings.Contains(out, "Password prompt") {
		t.Errorf("installer offered sudo for a non-permission failure:\n%s", out)
	}
	assertSudoNotInvoked(t, sudoLog)
}
