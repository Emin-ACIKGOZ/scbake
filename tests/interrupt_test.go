// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	interruptWait = 10 * time.Second
	tidyTimeout   = 15 * time.Second
)

// TestInterruptRollsBackAndExitsWithSignalStatus reproduces issue #130: a
// SIGINT/SIGTERM during a long child command must stop the child, roll back the
// partial project, and exit with the conventional 128+signal status.
func TestInterruptRollsBackAndExitsWithSignalStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal delivery is not available on Windows")
	}

	tests := []struct {
		name string
		sig  os.Signal
		want int
	}{
		{"SIGINT", os.Interrupt, fileutil.ExitInterrupted},
		{"SIGTERM", syscall.SIGTERM, fileutil.ExitTerminated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work := t.TempDir()
			installBlockingGo(t, work)

			cmd := exec.CommandContext(context.Background(), binaryPath, "new", "demo", "--lang", "go")
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(work, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))

			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output

			if err := cmd.Start(); err != nil {
				t.Fatalf("starting scbake: %v", err)
			}

			marker := filepath.Join(work, "demo", "tidy.started")
			waitForPath(t, marker, tidyTimeout)

			if err := cmd.Process.Signal(tt.sig); err != nil {
				_ = cmd.Process.Kill()
				t.Fatalf("sending %s: %v", tt.sig, err)
			}

			err := waitForCommand(t, cmd, interruptWait)
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("scbake did not fail after %s (err=%v, output=%s)", tt.sig, err, output.String())
			}
			if exitErr.ExitCode() != tt.want {
				t.Errorf("exit code = %d, want %d (output=%s)", exitErr.ExitCode(), tt.want, output.String())
			}

			if _, statErr := os.Stat(filepath.Join(work, "demo")); !os.IsNotExist(statErr) {
				t.Errorf("interrupted project directory was not rolled back (stat err: %v, output=%s)",
					statErr, output.String())
			}
		})
	}
}

// TestInterruptRestoresOverwrittenFile verifies that the SIGINT/SIGTERM rollback
// actually restores a pre-existing file from its backup, rather than only
// relying on `new` deleting the whole bootstrap directory.
//
//nolint:cyclop // End-to-end signal, backup, and rollback assertions.
func TestInterruptRestoresOverwrittenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal delivery is not available on Windows")
	}

	tests := []struct {
		name string
		sig  os.Signal
		want int
	}{
		{"SIGINT", os.Interrupt, fileutil.ExitInterrupted},
		{"SIGTERM", syscall.SIGTERM, fileutil.ExitTerminated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work := t.TempDir()
			installBlockingGo(t, work)

			project := filepath.Join(work, "project")
			if err := os.MkdirAll(project, 0o750); err != nil {
				t.Fatalf("creating project: %v", err)
			}
			if err := os.WriteFile(filepath.Join(project, "scbake.toml"), nil, 0o600); err != nil {
				t.Fatalf("writing manifest: %v", err)
			}
			if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/demo\n"), 0o600); err != nil {
				t.Fatalf("writing go.mod: %v", err)
			}
			const original = "package main\n\n// original content\n"
			mainPath := filepath.Join(project, "main.go")
			if err := os.WriteFile(mainPath, []byte(original), 0o600); err != nil {
				t.Fatalf("writing main.go: %v", err)
			}

			cmd := exec.CommandContext(context.Background(), binaryPath, "apply", "--lang", "go", "--force", ".")
			cmd.Dir = project
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(work, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))

			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output

			if err := cmd.Start(); err != nil {
				t.Fatalf("starting scbake: %v", err)
			}

			waitForPath(t, filepath.Join(project, "tidy.started"), tidyTimeout)

			overwritten, err := os.ReadFile(mainPath) //nolint:gosec // test-controlled path
			if err != nil {
				t.Fatalf("reading overwritten main.go: %v", err)
			}
			if string(overwritten) == original {
				t.Fatalf("main.go was not overwritten before the signal; backup would not be exercised")
			}

			if err := cmd.Process.Signal(tt.sig); err != nil {
				_ = cmd.Process.Kill()
				t.Fatalf("sending %s: %v", tt.sig, err)
			}

			waitErr := waitForCommand(t, cmd, interruptWait)
			exitErr, ok := waitErr.(*exec.ExitError)
			if !ok {
				t.Fatalf("scbake did not fail after %s (err=%v, output=%s)", tt.sig, waitErr, output.String())
			}
			if exitErr.ExitCode() != tt.want {
				t.Errorf("exit code = %d, want %d (output=%s)", exitErr.ExitCode(), tt.want, output.String())
			}

			restored, err := os.ReadFile(mainPath) //nolint:gosec // test-controlled path
			if err != nil {
				t.Fatalf("reading restored main.go: %v", err)
			}
			if string(restored) != original {
				t.Errorf("main.go after %s = %q, want the original %q", tt.sig, restored, original)
			}

			if _, statErr := os.Stat(filepath.Join(project, ".scbake")); !os.IsNotExist(statErr) {
				t.Errorf("transaction state was not cleaned up after %s (stat err: %v)", tt.sig, statErr)
			}
		})
	}
}

// installBlockingGo writes a fake go binary that blocks on `go mod tidy` so the
// test has a deterministic window in which to deliver a signal.
func installBlockingGo(t *testing.T, work string) {
	t.Helper()

	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go is required for this test: %v", err)
	}

	binDir := filepath.Join(work, "bin")
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		t.Fatalf("creating fake bin dir: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *"mod tidy"*) : > tidy.started; exec sleep 30 ;;
esac
exec %s "$@"
`, realGo)
	path := filepath.Join(binDir, "go")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // test helper must be executable
		t.Fatalf("writing fake go: %v", err)
	}
}

// waitForPath waits until path exists or the deadline expires.
func waitForPath(t *testing.T, path string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// waitForCommand waits for cmd to exit, killing it if it ignores the signal.
func waitForCommand(t *testing.T, cmd *exec.Cmd, timeout time.Duration) error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("scbake did not exit within %s after a signal", timeout)
		return nil
	}
}

// TestSIGKILLIsReportedAndRecovered covers the crash case that cannot be
// handled in-process: the next run must report the leftover journal, drift must
// not report the tree as clean, and `recover` must roll the transaction back.
//
//nolint:cyclop // End-to-end crash, drift, and recovery assertions.
func TestSIGKILLIsReportedAndRecovered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGKILL is not available on Windows")
	}

	work := t.TempDir()
	installBlockingGo(t, work)

	cmd := exec.CommandContext(t.Context(), binaryPath, "new", "demo", "--lang", "go")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(work, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting scbake: %v", err)
	}

	waitForPath(t, filepath.Join(work, "demo", "tidy.started"), tidyTimeout)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("SIGKILL: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("SIGKILL unexpectedly reported success")
	}

	project := filepath.Join(work, "demo")
	output, err := runInDir(t, project, "drift")
	if err == nil {
		t.Fatalf("drift reported a killed transaction as clean: %s", output)
	}
	if !strings.Contains(output, "incomplete transaction") {
		t.Errorf("drift output does not mention the incomplete transaction: %s", output)
	}

	output, err = runInDir(t, project, "apply", "--dry-run", "--lang", "go", ".")
	if err == nil {
		t.Fatalf("apply --dry-run accepted a killed transaction: %s", output)
	}
	if !strings.Contains(output, "incomplete transaction") {
		t.Errorf("apply --dry-run output does not mention the incomplete transaction: %s", output)
	}

	output, err = runInDir(t, project, "recover", "--dry-run")
	if err != nil {
		t.Fatalf("recover --dry-run failed: %v\nOutput: %s", err, output)
	}
	if !strings.Contains(output, "Would recover transaction") {
		t.Errorf("recover --dry-run output = %q", output)
	}
	if _, statErr := os.Stat(filepath.Join(project, "main.go")); statErr != nil {
		t.Errorf("recover --dry-run removed a partial file: %v", statErr)
	}

	output, err = runInDir(t, work, "recover", "demo")
	if err != nil {
		t.Fatalf("recover failed: %v\nOutput: %s", err, output)
	}
	if !strings.Contains(output, "Recovered 1 interrupted transaction") {
		t.Errorf("recover output = %q", output)
	}
	if _, statErr := os.Stat(project); !os.IsNotExist(statErr) {
		t.Errorf("recovery left the bootstrapped project directory: %v", statErr)
	}
}

// TestRecoverTerminatesOnSignal verifies that a SIGTERM delivered while
// recover is replaying pending transactions is not swallowed: recovery stops
// and the process exits with the conventional 128+signal status instead of 0.
func TestRecoverTerminatesOnSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal delivery is not available on Windows")
	}

	project := t.TempDir()
	writePendingTransactions(t, project, 300, 500)

	cmd := exec.CommandContext(context.Background(), binaryPath, "recover", "--force", project) // #nosec G204 -- test-controlled binary path
	cmd.Dir = project
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting recover: %v", err)
	}

	// Let the Go runtime install its signal handler; the pending workload is
	// large enough that recovery is still running when the signal arrives.
	time.Sleep(50 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("sending SIGTERM: %v", err)
	}

	err := waitForCommand(t, cmd, interruptWait)
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("recover completed before SIGTERM (err=%v, output=%s)", err, output.String())
	}
	if exitErr.ExitCode() != fileutil.ExitTerminated {
		t.Errorf("exit code = %d, want %d (output=%s)",
			exitErr.ExitCode(), fileutil.ExitTerminated, output.String())
	}
}

// TestRecoverDryRunShowsRootRemoval verifies that a dry run calls out a
// remove_root journal explicitly, because replaying an untrusted one with
// --force deletes the whole checkout.
func TestRecoverDryRunShowsRootRemoval(t *testing.T) {
	project := t.TempDir()
	dir := filepath.Join(project, ".scbake", "tmp", "tx-new")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	header := fmt.Sprintf(
		`{"version":1,"pid":%d,"started":%q,"remove_root":true}`,
		1<<30,
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	)
	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte(header+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := runInDir(t, project, "recover", "--dry-run", "--force")
	if err != nil {
		t.Fatalf("recover --dry-run failed: %v\nOutput: %s", err, output)
	}
	if !strings.Contains(output, "entire project root") {
		t.Errorf("recover --dry-run did not disclose root removal: %q", output)
	}
}

// writePendingTransactions creates count pending transaction directories, each
// with entries unique created paths, so recovery has deterministic work to do.
func writePendingTransactions(t *testing.T, project string, count, entries int) {
	t.Helper()

	tmpDir := filepath.Join(project, ".scbake", "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		t.Fatalf("creating transaction parent: %v", err)
	}

	header := fmt.Sprintf(
		`{"version":1,"pid":%d,"started":%q}`,
		1<<30,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339),
	)
	var body strings.Builder
	for dirIndex := range count {
		body.Reset()
		body.WriteString(header)
		body.WriteByte('\n')
		for entryIndex := range entries {
			fmt.Fprintf(&body, `{"op":"create","path":"generated/file-%d-%d"}`+"\n", dirIndex, entryIndex)
		}

		dir := filepath.Join(tmpDir, fmt.Sprintf("tx-%d", dirIndex))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("creating transaction %d: %v", dirIndex, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte(body.String()), 0o600); err != nil {
			t.Fatalf("writing journal %d: %v", dirIndex, err)
		}
	}
}

// runInDir runs the scbake binary in dir and returns combined output.
func runInDir(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binaryPath, args...) //nolint:gosec // test binary path
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}
