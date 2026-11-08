// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
	"time"
)

const (
	cmdLockHelperEnv  = "SCBAKE_CMD_LOCK_HELPER"
	cmdLockReadyName  = "cmd-lock-helper-ready"
	cmdLockHelperWait = 10 * time.Second
)

// TestReadOnlyCommandsRunWhileRootLockHeld verifies drift, list, apply
// --dry-run, and template registry commands never take the project mutation
// lock. If any of them did, they would fail with ErrBusy here.
func TestReadOnlyCommandsRunWhileRootLockHeld(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), nil, fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	lock, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("AcquireRootLock failed: %v", err)
	}
	defer func() { _ = lock.Release() }()

	t.Chdir(root)

	if err := executeCLI("drift"); err != nil {
		t.Errorf("drift while locked failed: %v", err)
	}
	if err := executeCLI("list", "projects"); err != nil {
		t.Errorf("list projects while locked failed: %v", err)
	}
	if err := executeCLI("apply", "--dry-run", "--with", "editorconfig", "."); err != nil {
		t.Errorf("apply --dry-run while locked failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".editorconfig")); !os.IsNotExist(err) {
		t.Errorf("apply --dry-run created .editorconfig (stat err: %v)", err)
	}

	isolateXDGDirs(t)
	if err := executeCLI("template", "registry", "add", "local", "https://example.invalid/templates.zip"); err != nil {
		t.Errorf("template registry add while locked failed: %v", err)
	}
	if err := executeCLI("template", "registry", "list"); err != nil {
		t.Errorf("template registry list while locked failed: %v", err)
	}
}

// TestMissingTargetCommandsFailWithoutCreatingRoot verifies apply, update, and
// drift --repair on a missing target exit with an ordinary error (not busy, not
// exit 75) and do not materialize an empty project root.
func TestMissingTargetCommandsFailWithoutCreatingRoot(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"apply", []string{"apply", "--with", "editorconfig"}},
		{"update", []string{"update"}},
		{"drift repair", []string{"drift", "--repair"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetFlags()
			t.Cleanup(resetFlags)

			missing := filepath.Join(t.TempDir(), "missing")
			args := append(append([]string{}, tc.args...), missing)
			err := executeCLI(args...)
			if err == nil {
				t.Fatal("command on a missing target unexpectedly succeeded")
			}
			if errors.Is(err, transaction.ErrBusy) {
				t.Fatalf("%s on a missing target returned ErrBusy: %v", tc.name, err)
			}
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s error = %v, want fs.ErrNotExist", tc.name, err)
			}
			if code := commandExitCode(err, nil); code != fileutil.ExitError {
				t.Errorf("%s exit code = %d, want %d (not %d)", tc.name, code, fileutil.ExitError, fileutil.ExitBusy)
			}
			if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
				t.Errorf("%s created the missing root (stat err: %v)", tc.name, statErr)
			}
		})
	}
}

// TestNewCleanupLockHelperProcess is the child half of the failed-new cleanup
// test. It holds the root lock in its working directory until killed.
func TestNewCleanupLockHelperProcess(t *testing.T) {
	if os.Getenv(cmdLockHelperEnv) != "1" {
		t.Skip("helper process only")
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper getwd failed: %v\n", err)
		os.Exit(2)
	}

	lock, err := transaction.AcquireRootLock(root, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper acquire failed: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(filepath.Join(root, cmdLockReadyName), []byte("ready"), fileutil.PrivateFilePerms); err != nil {
		_ = lock.Release()
		fmt.Fprintf(os.Stderr, "helper ready marker failed: %v\n", err)
		os.Exit(2)
	}
	for {
		time.Sleep(time.Hour)
	}
}

// TestCleanupFailedProjectKeepsDirectoryWhileLocked verifies failed-new cleanup
// re-acquires the root lock and refuses to delete a project another process is
// using.
func TestCleanupFailedProjectKeepsDirectoryWhileLocked(t *testing.T) {
	project := t.TempDir()
	ready := filepath.Join(project, cmdLockReadyName)

	//nolint:gosec // Re-exec the current test binary as a lock helper.
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestNewCleanupLockHelperProcess$", "--")
	cmd.Dir = project
	cmd.Env = append(os.Environ(), cmdLockHelperEnv+"=1")
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting lock helper: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	waitForCmdFile(t, ready, cmdLockHelperWait)

	stderr := captureStderr(t, func() { cleanupFailedProject(project) })
	if !strings.Contains(stderr, "Not removing") {
		t.Errorf("cleanup stderr = %q, want a refusal to remove the locked project", stderr)
	}
	if _, err := os.Stat(project); err != nil {
		t.Fatalf("cleanup removed a project another process holds: %v", err)
	}

	probe, err := transaction.AcquireRootLock(project, false)
	if !errors.Is(err, transaction.ErrBusy) {
		if probe != nil {
			_ = probe.Release()
		}
		t.Fatalf("helper lock was not still held after cleanup: %v", err)
	}
}

func waitForCmdFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// assertNoFailedProjectStaging fails when a cleanup staging directory is left
// behind in dir.
func assertNoFailedProjectStaging(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".scbake-remove-") {
			t.Errorf("cleanup left staging directory %s", filepath.Join(dir, entry.Name()))
		}
	}
}

// TestRemoveFailedProjectReleasesLockBeforeRename verifies failed-new cleanup
// creates the staging directory, releases the lock while the original project
// is still in place and staging is empty, then renames and deletes the staged
// tree. Windows cannot rename a directory while its lock handle is open.
//
//nolint:cyclop // Lock setup, ordering assertions from the release callback, and cleanup checks.
func TestRemoveFailedProjectReleasesLockBeforeRename(t *testing.T) {
	parent := t.TempDir()
	project := filepath.Join(parent, "demo")
	if err := os.Mkdir(project, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "partial.txt"), []byte("partial"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	lock, err := transaction.AcquireRootLock(project, false)
	if err != nil {
		t.Fatalf("AcquireRootLock failed: %v", err)
	}

	released := false
	err = removeFailedProject(project, func() error {
		released = true
		if _, statErr := os.Stat(project); statErr != nil {
			t.Errorf("project was renamed before the lock was released: %v", statErr)
		}
		entries, readErr := os.ReadDir(parent)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".scbake-remove-") {
				continue
			}
			staging := filepath.Join(parent, entry.Name())
			staged, readErr := os.ReadDir(staging)
			if readErr != nil {
				return readErr
			}
			if len(staged) != 0 {
				t.Errorf("project was moved into staging before the lock was released: %v", staged)
			}
			return lock.Release()
		}
		return errors.New("staging directory was missing at release")
	})
	if err != nil {
		_ = lock.Release()
		t.Fatalf("removeFailedProject failed: %v", err)
	}
	if !released {
		t.Fatal("removeFailedProject did not call release")
	}
	if _, err := os.Lstat(project); !os.IsNotExist(err) {
		t.Errorf("removeFailedProject left the original project (stat err: %v)", err)
	}
	assertNoFailedProjectStaging(t, parent)
}
