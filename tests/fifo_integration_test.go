// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fifoCommandTimeout bounds each CLI invocation. Before issue #218 was fixed,
// the process blocked forever opening the FIFO, so the context deadline is what
// turns a regression into a prompt, diagnosable test failure.
const fifoCommandTimeout = 20 * time.Second

// TestManagedFIFOExitsPromptly is an end-to-end regression test for issue #218:
// a managed file replaced by a FIFO must make every command that reads, backs up
// or rewrites it fail fast with a clear diagnostic instead of blocking on open.
func TestManagedFIFOExitsPromptly(t *testing.T) {
	work := t.TempDir()
	newFIFOProject(t, work, "p")

	project := filepath.Join(work, "p")
	replaceWithFIFO(t, filepath.Join(project, ".editorconfig"))

	tests := []struct {
		name string
		dir  string
		args []string
	}{
		{"drift", work, []string{"drift", "p"}},
		{"update dry-run", work, []string{"update", "--dry-run", "p"}},
		{"update", work, []string{"update", "p"}},
		{"update force", work, []string{"update", "--force", "p"}},
		{"apply force", project, []string{"apply", "--with", "editorconfig", "--force", "."}},
		{"accept", project, []string{"accept", ".editorconfig"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireNonRegularFailure(t, tt.dir, tt.args...)
		})
	}
}

// TestManagedFIFOBaseExitsPromptly covers the other bypass: a committed base
// shadow replaced by a FIFO. Default update must not treat the unreadable base
// as usable, and the transaction backup of the base must fail fast instead of
// blocking on the FIFO.
func TestManagedFIFOBaseExitsPromptly(t *testing.T) {
	work := t.TempDir()
	runCLIOrFail(t, work, "new", "p", "--with", "compliance",
		"--license", "MIT", "--copyright-holder", "Test Corp")

	project := filepath.Join(work, "p")
	replaceWithFIFO(t, filepath.Join(project, ".scbake-base", "LICENSE"))

	requireNonRegularFailure(t, work, "update", "p")
}

// TestManagedFIFOBaseNoOpUpdateExitsPromptly covers the reconciliation-time
// bypass: when the template is unchanged the merge strategy returns a no-op
// before reading the base, which used to let `update` exit 0 with a FIFO base.
// The no-op decision must still validate that an existing base leaf is a
// regular file, for both a real and a dry-run update.
func TestManagedFIFOBaseNoOpUpdateExitsPromptly(t *testing.T) {
	work := t.TempDir()
	newFIFOProject(t, work, "p")

	project := filepath.Join(work, "p")
	replaceWithFIFO(t, filepath.Join(project, ".scbake-base", ".editorconfig"))

	for _, args := range [][]string{
		{"update", "p"},
		{"update", "--dry-run", "p"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			requireNonRegularFailure(t, work, args...)
		})
	}
}

// requireNonRegularFailure runs the CLI and asserts that it exits non-zero
// promptly with a non-regular-file diagnostic rather than being killed by the
// test deadline.
func requireNonRegularFailure(t *testing.T, dir string, args ...string) {
	t.Helper()

	out, timedOut, err := runCLIInDir(t, dir, fifoCommandTimeout, args...)
	if timedOut {
		t.Fatalf("scbake %s blocked for %s; output: %s", strings.Join(args, " "), fifoCommandTimeout, out)
	}
	if err == nil {
		t.Fatalf("scbake %s succeeded with a FIFO; output: %s", strings.Join(args, " "), out)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("scbake %s did not exit with a non-zero status: %v; output: %s",
			strings.Join(args, " "), err, out)
	}
	if exitErr.ExitCode() == 0 {
		t.Errorf("scbake %s exited 0 with a FIFO; output: %s", strings.Join(args, " "), out)
	}
	if !strings.Contains(out, "not a regular file") {
		t.Errorf("scbake %s output does not mention %q: %s",
			strings.Join(args, " "), "not a regular file", out)
	}
}

// newFIFOProject creates a project with a managed .editorconfig via the CLI.
func newFIFOProject(t *testing.T, work, name string) {
	t.Helper()
	runCLIOrFail(t, work, "new", name, "--with", "editorconfig")
}

// runCLIOrFail runs the CLI and fails the test on a timeout or error.
func runCLIOrFail(t *testing.T, dir string, args ...string) {
	t.Helper()

	out, timedOut, err := runCLIInDir(t, dir, fifoCommandTimeout, args...)
	if timedOut {
		t.Fatalf("scbake %s timed out after %s; output: %s", strings.Join(args, " "), fifoCommandTimeout, out)
	}
	if err != nil {
		t.Fatalf("scbake %s failed: %v\nOutput: %s", strings.Join(args, " "), err, out)
	}
}

// replaceWithFIFO removes path and creates a FIFO in its place.
func replaceWithFIFO(t *testing.T, path string) {
	t.Helper()

	if err := os.Remove(path); err != nil {
		t.Fatalf("removing %s: %v", path, err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("creating FIFO at %s: %v", path, err)
	}
}

// runCLIInDir executes the compiled binary in dir with a context deadline,
// returning combined output, whether the deadline fired, and the command error.
func runCLIInDir(t *testing.T, dir string, timeout time.Duration, args ...string) (string, bool, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...) //nolint:gosec // test binary path
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test Runner",
		"GIT_AUTHOR_EMAIL=runner@test.com",
		"GIT_COMMITTER_NAME=Test Runner",
		"GIT_COMMITTER_EMAIL=runner@test.com",
	)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	return output.String(), ctx.Err() != nil, err
}
