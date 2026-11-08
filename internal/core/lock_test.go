// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/ui"
	"scbake/internal/util/fileutil"
	"scbake/pkg/templates"
	"testing"
)

const lockFailureTemplate = "test-lock-failure"

// lockFailureTask writes and tracks a file, then fails, exercising the
// deferred rollback path while the root lock is held.
type lockFailureTask struct {
	path string
}

func (t *lockFailureTask) Description() string { return "injected lock failure task" }

func (t *lockFailureTask) Priority() int { return int(types.PrioConfigUniversal) }

func (t *lockFailureTask) Execute(tc types.TaskContext) error {
	if tc.Tx != nil {
		if err := tc.Tx.Track(t.path); err != nil {
			return err
		}
	}
	if err := os.WriteFile(t.path, []byte("partial"), fileutil.PrivateFilePerms); err != nil {
		return err
	}
	return errors.New("injected apply failure")
}

type lockFailureHandler struct{}

func (lockFailureHandler) GetTasks(targetPath, _, _ string) ([]types.Task, error) {
	return []types.Task{&lockFailureTask{path: filepath.Join(targetPath, "lock-failure.txt")}}, nil
}

func writeLockTestManifest(t *testing.T, root string, content string) string {
	t.Helper()
	path := filepath.Join(root, fileutil.ManifestFileName)
	if err := os.WriteFile(path, []byte(content), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	return path
}

func lockTestRunContext(root string, with ...string) RunContext {
	return RunContext{
		TargetPath:       root,
		ManifestPathArg:  ".",
		WithFlag:         with,
		Force:            true,
		ConflictStrategy: types.ConflictOverwrite,
	}
}

// TestRunApply_BusyBeforeManifestLoad verifies the lock is acquired before the
// manifest is decoded or any project file is touched. The manifest deliberately
// contains invalid TOML: a busy run must report ErrBusy, not a decode error.
func TestRunApply_BusyBeforeManifestLoad(t *testing.T) {
	root := t.TempDir()
	const sentinel = "this is not valid toml ="
	manifestPath := writeLockTestManifest(t, root, sentinel)

	lock, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("AcquireRootLock failed: %v", err)
	}
	defer func() { _ = lock.Release() }()

	reporter := ui.NewPlainReporter(5, false)
	err = RunApply(lockTestRunContext(root, "editorconfig"), reporter)
	if !errors.Is(err, transaction.ErrBusy) {
		t.Fatalf("RunApply error = %v, want ErrBusy (manifest must not be loaded first)", err)
	}

	content, readErr := os.ReadFile(manifestPath) //nolint:gosec // test-controlled path
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != sentinel {
		t.Errorf("busy apply rewrote the manifest: %q", content)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".editorconfig")); !os.IsNotExist(statErr) {
		t.Errorf("busy apply created a target file (stat err: %v)", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir)); !os.IsNotExist(statErr) {
		t.Errorf("busy apply created transaction state (stat err: %v)", statErr)
	}
}

// TestRunApply_ReleasesLockAfterSuccess verifies a committed apply releases the
// lock and leaves the existing .scbake cleanup behavior intact.
func TestRunApply_ReleasesLockAfterSuccess(t *testing.T) {
	root := t.TempDir()
	writeLockTestManifest(t, root, "")

	reporter := ui.NewPlainReporter(5, false)
	if err := RunApply(lockTestRunContext(root, "editorconfig"), reporter); err != nil {
		t.Fatalf("RunApply failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".editorconfig")); err != nil {
		t.Errorf("successful apply did not create .editorconfig: %v", err)
	}
	lockPath := filepath.Join(root, fileutil.InternalDir, fileutil.LockFileName)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("successful apply left the lock file behind (stat err: %v)", err)
	}

	probe, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("lock was not released after commit: %v", err)
	}
	if err := probe.Release(); err != nil {
		t.Fatalf("Release() of probe failed: %v", err)
	}
}

// TestRunApply_ReleasesLockAfterRollback verifies rollback completes while the
// lock is held and the lock is available for a second apply afterwards.
func TestRunApply_ReleasesLockAfterRollback(t *testing.T) {
	root := t.TempDir()
	writeLockTestManifest(t, root, "")
	templates.Register(lockFailureTemplate, lockFailureHandler{})

	reporter := ui.NewPlainReporter(5, false)
	err := RunApply(lockTestRunContext(root, lockFailureTemplate), reporter)
	if err == nil {
		t.Fatal("failing apply unexpectedly succeeded")
	}
	if errors.Is(err, transaction.ErrBusy) {
		t.Fatalf("failing apply returned ErrBusy, not its task error: %v", err)
	}

	created := filepath.Join(root, "lock-failure.txt")
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("rollback left created file %s (stat err: %v)", created, err)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir)); !os.IsNotExist(err) {
		t.Errorf("failed apply left .scbake residue (stat err: %v)", err)
	}

	probe, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("lock was not released after rollback: %v", err)
	}
	if err := probe.Release(); err != nil {
		t.Fatalf("Release() of probe failed: %v", err)
	}

	if err := RunApply(lockTestRunContext(root, "editorconfig"), reporter); err != nil {
		t.Fatalf("second apply after rollback failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".editorconfig")); err != nil {
		t.Errorf("second apply did not create .editorconfig: %v", err)
	}
}

// TestRunUpdateSharesSingleRootLock verifies update does not re-acquire the
// lock in the same process; a nested acquire would return ErrBusy.
func TestRunUpdateSharesSingleRootLock(t *testing.T) {
	root := t.TempDir()
	writeLockTestManifest(t, root, "")

	reporter := ui.NewPlainReporter(5, false)
	if err := RunApply(lockTestRunContext(root, "editorconfig"), reporter); err != nil {
		t.Fatalf("initial apply failed: %v", err)
	}

	rc := RunContext{
		TargetPath:       root,
		ManifestPathArg:  ".",
		Force:            true,
		ConflictStrategy: types.ConflictOverwrite,
	}
	if err := RunUpdate(rc, reporter); err != nil {
		t.Fatalf("RunUpdate failed (possible double lock): %v", err)
	}
	lockPath := filepath.Join(root, fileutil.InternalDir, fileutil.LockFileName)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("update left the lock file behind (stat err: %v)", err)
	}

	probe, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("lock was not released after update: %v", err)
	}
	if err := probe.Release(); err != nil {
		t.Fatalf("Release() of probe failed: %v", err)
	}
}

// TestRunApply_DryRunIgnoresRootLock verifies read-only dry runs do not contend
// with a mutating process and still write nothing.
func TestRunApply_DryRunIgnoresRootLock(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeLockTestManifest(t, root, "")

	lock, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("AcquireRootLock failed: %v", err)
	}
	defer func() { _ = lock.Release() }()

	rc := lockTestRunContext(root, "editorconfig")
	rc.DryRun = true
	reporter := ui.NewPlainReporter(5, true)
	if err := RunApply(rc, reporter); err != nil {
		t.Fatalf("dry-run apply while locked failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".editorconfig")); !os.IsNotExist(err) {
		t.Errorf("dry-run apply created .editorconfig (stat err: %v)", err)
	}

	content, err := os.ReadFile(manifestPath) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Errorf("dry-run apply rewrote the manifest: %q", content)
	}
}
