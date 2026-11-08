// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
	"time"
)

const (
	lockHelperReadyName = "lock-helper-ready"
	lockHelperWait      = 10 * time.Second

	testStateDirPerm os.FileMode = 0o700
)

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be absent, got err=%v", path, err)
	}
}

func acquireForTest(t *testing.T, root string) *RootLock {
	t.Helper()
	lock, err := AcquireRootLock(root)
	if err != nil {
		t.Fatalf("AcquireRootLock(%s) failed: %v", root, err)
	}
	return lock
}

// TestAcquireRootLock_BusyThenReacquire verifies the central lock invariant: a
// second holder is rejected with ErrBusy while the first is held, and the root
// is lockable again immediately after release.
func TestAcquireRootLock_BusyThenReacquire(t *testing.T) {
	root := t.TempDir()

	first := acquireForTest(t, root)
	defer func() { _ = first.Release() }()

	second, err := AcquireRootLock(root)
	if !errors.Is(err, ErrBusy) {
		if second != nil {
			_ = second.Release()
		}
		t.Fatalf("second AcquireRootLock() error = %v, want ErrBusy", err)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}

	reacquired := acquireForTest(t, root)
	if err := reacquired.Release(); err != nil {
		t.Fatalf("Release() after reacquire failed: %v", err)
	}
}

// TestAcquireRootLock_StaleFileHarmless ensures a leftover lock file from a
// crash is not treated as an active lock; kernel state is authoritative.
func TestAcquireRootLock_StaleFileHarmless(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, fileutil.InternalDir)
	if err := os.MkdirAll(stateDir, testStateDirPerm); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(stateDir, fileutil.LockFileName)
	if err := os.WriteFile(lockPath, nil, fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	lock := acquireForTest(t, root)
	if err := lock.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}
}

// TestAcquireRootLock_MissingRootPolicy verifies the createRoot parameter: only
// commands that bootstrap a project may create the root, while commands that
// operate on an existing project fail without leaving an empty directory.
func TestAcquireRootLock_MissingRootPolicy(t *testing.T) {
	parent := t.TempDir()

	t.Run("existing project does not create", func(t *testing.T) {
		root := filepath.Join(parent, "missing")
		lock, err := AcquireRootLock(root)
		if lock != nil {
			_ = lock.Release()
			t.Fatal("AcquireRootLock returned a lock for a missing root")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("AcquireRootLock error = %v, want fs.ErrNotExist", err)
		}
		assertNotExist(t, root)
	})

	t.Run("new may create", func(t *testing.T) {
		root := filepath.Join(parent, "created")
		lock, err := AcquireRootLock(root, true)
		if err != nil {
			t.Fatalf("AcquireRootLock(createRoot=true) failed: %v", err)
		}
		if _, err := os.Stat(root); err != nil {
			t.Errorf("AcquireRootLock(createRoot=true) did not create the root: %v", err)
		}
		if err := lock.Release(); err != nil {
			t.Fatalf("Release() failed: %v", err)
		}
		assertNotExist(t, filepath.Join(root, fileutil.InternalDir))
	})
}

// TestAcquireRootLock_CreatesPrivateState verifies the state directory and lock
// file are created owner-only so a shared repository does not expose them.
func TestAcquireRootLock_CreatesPrivateState(t *testing.T) {
	root := t.TempDir()
	lock := acquireForTest(t, root)
	defer func() { _ = lock.Release() }()

	if runtime.GOOS == osWindows {
		return // Windows does not expose POSIX mode bits.
	}

	stateDir := filepath.Join(root, fileutil.InternalDir)
	stateInfo, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("stat state dir: %v", err)
	}
	if got := stateInfo.Mode().Perm(); got != testStateDirPerm {
		t.Errorf("%s mode = %o, want %o", stateDir, got, testStateDirPerm)
	}

	lockInfo, err := os.Stat(filepath.Join(stateDir, fileutil.LockFileName))
	if err != nil {
		t.Fatalf("stat lock file: %v", err)
	}
	if got := lockInfo.Mode().Perm(); got != fileutil.PrivateFilePerms {
		t.Errorf("lock mode = %o, want %o", got, fileutil.PrivateFilePerms)
	}
}

// TestRootLock_ReleaseRemovesState verifies release removes the lock file and
// prunes an empty .scbake directory, preserving the existing cleanup contract.
func TestRootLock_ReleaseRemovesState(t *testing.T) {
	root := t.TempDir()
	lock := acquireForTest(t, root)

	lockPath := filepath.Join(root, fileutil.InternalDir, fileutil.LockFileName)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file missing while held: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}
	assertNotExist(t, lockPath)
	assertNotExist(t, filepath.Join(root, fileutil.InternalDir))
}

// TestRootLock_ReleaseKeepsNonEmptyState verifies release never deletes state
// that does not belong to the lock (the journal shares .scbake).
func TestRootLock_ReleaseKeepsNonEmptyState(t *testing.T) {
	root := t.TempDir()
	lock := acquireForTest(t, root)

	stateDir := filepath.Join(root, fileutil.InternalDir)
	keep := filepath.Join(stateDir, "keep.txt")
	if err := os.WriteFile(keep, []byte("journal state"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}

	assertNotExist(t, filepath.Join(stateDir, fileutil.LockFileName))
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("Release removed unrelated state %s: %v", keep, err)
	}
	if _, err := os.Stat(stateDir); err != nil {
		t.Errorf("Release removed non-empty state dir %s: %v", stateDir, err)
	}
}

// TestAcquireRootLock_RejectsSymlinkEscape verifies a symlinked state directory
// or lock leaf cannot redirect the lock outside the project root.
//
//nolint:cyclop // Three distinct symlink escape scenarios in one test.
func TestAcquireRootLock_RejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	t.Run("state directory escape", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, fileutil.InternalDir)); err != nil {
			t.Fatal(err)
		}

		if lock, err := AcquireRootLock(root); err == nil {
			_ = lock.Release()
			t.Fatal("AcquireRootLock accepted a symlinked state directory")
		}
		assertNotExist(t, filepath.Join(outside, fileutil.LockFileName))
	})

	t.Run("lock leaf escape", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside-lock")
		if err := os.WriteFile(outside, []byte("outside"), fileutil.PrivateFilePerms); err != nil {
			t.Fatal(err)
		}
		stateDir := filepath.Join(root, fileutil.InternalDir)
		if err := os.MkdirAll(stateDir, testStateDirPerm); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(stateDir, fileutil.LockFileName)); err != nil {
			t.Fatal(err)
		}

		if lock, err := AcquireRootLock(root); err == nil {
			_ = lock.Release()
			t.Fatal("AcquireRootLock accepted an escaping lock symlink")
		}
		content, err := os.ReadFile(outside) //nolint:gosec // test-controlled path
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "outside" {
			t.Errorf("outside lock file content = %q, want it untouched", content)
		}
	})

	t.Run("lock leaf symlink inside root", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "inside-lock")
		if err := os.WriteFile(target, []byte("inside"), fileutil.PrivateFilePerms); err != nil {
			t.Fatal(err)
		}
		stateDir := filepath.Join(root, fileutil.InternalDir)
		if err := os.MkdirAll(stateDir, testStateDirPerm); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(stateDir, fileutil.LockFileName)); err != nil {
			t.Fatal(err)
		}

		if lock, err := AcquireRootLock(root); err == nil {
			_ = lock.Release()
			t.Fatal("AcquireRootLock followed a symlinked lock leaf")
		}
	})
}

// TestAcquireRootLock_RevalidatesReplacedPath covers the unlink/revalidation
// primitive used to close the remove-on-release race: a handle whose path was
// replaced is not considered valid and Release must not delete the replacement.
//
// The old name is freed with a rename rather than DeleteFile. Windows allows a
// rename while a share-delete handle is open, but DeleteFile would leave the
// path delete-pending and reject the replacement create with access-denied.
func TestAcquireRootLock_RevalidatesReplacedPath(t *testing.T) {
	root := t.TempDir()
	lock := acquireForTest(t, root)
	defer func() { _ = lock.Release() }()

	if !lockPathMatches(lock.file, lock.path) {
		t.Fatal("freshly acquired lock does not match its path")
	}

	oldPath := lock.path + ".old"
	if err := os.Rename(lock.path, oldPath); err != nil {
		t.Fatalf("freeing the old lock name: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(oldPath) })
	replacement := []byte("replacement-lock")
	if err := os.WriteFile(lock.path, replacement, fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("creating the replacement lock: %v", err)
	}
	if lockPathMatches(lock.file, lock.path) {
		t.Fatal("a replaced lock path still matched the old handle")
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}
	content, err := os.ReadFile(lock.path)
	if err != nil {
		t.Fatalf("Release removed the replacement lock file: %v", err)
	}
	if string(content) != string(replacement) {
		t.Errorf("replacement lock content = %q, want %q", content, replacement)
	}

	// The replacement is a real file with no holder, so a new acquire succeeds
	// and release cleans it up normally.
	next := acquireForTest(t, root)
	if err := next.Release(); err != nil {
		t.Fatalf("Release() of replacement failed: %v", err)
	}
	assertNotExist(t, lock.path)
}

// TestAcquireRootLock_SymlinkedRootContends verifies canonicalization: a
// symlinked spelling of the same root contends with the real path.
func TestAcquireRootLock_SymlinkedRootContends(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	realRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "project-link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}

	lock := acquireForTest(t, realRoot)
	defer func() { _ = lock.Release() }()

	alias, err := AcquireRootLock(link)
	if !errors.Is(err, ErrBusy) {
		if alias != nil {
			_ = alias.Release()
		}
		t.Fatalf("AcquireRootLock through symlink error = %v, want ErrBusy", err)
	}
}

// TestRootLockHelperProcess is the child half of the crash-release test. It is
// a no-op unless the parent starts the test binary in helper mode.
func TestRootLockHelperProcess(t *testing.T) {
	root, err := os.Getwd()
	if err != nil || os.Getenv("SCBAKE_LOCK_HELPER") != "1" {
		t.Skip("helper process only")
	}

	lock, err := AcquireRootLock(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper acquire failed: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(filepath.Join(root, lockHelperReadyName), []byte("ready"), fileutil.PrivateFilePerms); err != nil {
		_ = lock.Release()
		fmt.Fprintf(os.Stderr, "helper ready marker failed: %v\n", err)
		os.Exit(2)
	}

	// Wait to be killed. Sleep keeps a timer registered so the runtime does not
	// report a deadlock while the parent holds the process open.
	for {
		time.Sleep(time.Hour)
	}
}

// TestAcquireRootLock_ReleasesAfterKill verifies the kernel releases an
// advisory lock when the holder is SIGKILLed, so the next run is not blocked by
// a stale lock file.
//
//nolint:cyclop // Process start, busy check, kill, and bounded reacquire.
func TestAcquireRootLock_ReleasesAfterKill(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("POSIX SIGKILL is not available on Windows")
	}

	root := t.TempDir()
	ready := filepath.Join(root, lockHelperReadyName)

	//nolint:gosec // Re-exec the current test binary as a lock helper.
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRootLockHelperProcess$", "--")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SCBAKE_LOCK_HELPER=1")
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

	waitForFile(t, ready, lockHelperWait)

	if other, err := AcquireRootLock(root); !errors.Is(err, ErrBusy) {
		if other != nil {
			_ = other.Release()
		}
		t.Fatalf("AcquireRootLock while helper holds it = %v, want ErrBusy", err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("killing lock helper: %v", err)
	}
	_ = cmd.Wait()

	deadline := time.Now().Add(lockHelperWait)
	for {
		lock, err := AcquireRootLock(root)
		if err == nil {
			if releaseErr := lock.Release(); releaseErr != nil {
				t.Fatalf("Release after crash failed: %v", releaseErr)
			}
			return
		}
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("AcquireRootLock after kill = %v, want success or ErrBusy", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock was not released within %s after SIGKILL; output: %s", lockHelperWait, output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
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

// TestRecoverCleanDoesNotCreateState verifies a clean recover returns without
// acquiring the lock or creating .scbake, for both normal and discard modes.
func TestRecoverCleanDoesNotCreateState(t *testing.T) {
	for _, discard := range []bool{false, true} {
		t.Run(fmt.Sprintf("discard=%t", discard), func(t *testing.T) {
			root := t.TempDir()
			recovered, err := RecoverContext(context.Background(), root, discard)
			if err != nil {
				t.Fatalf("clean recover failed: %v", err)
			}
			if recovered != 0 {
				t.Errorf("clean recover handled %d transactions, want 0", recovered)
			}
			assertNotExist(t, filepath.Join(root, fileutil.InternalDir))
		})
	}
}

// TestRecoverBusyDoesNotMutate verifies recover and recover --discard take the
// root lock when transaction state exists and fail busy before touching it.
//
//nolint:cyclop // Covers both recovery modes plus the post-release retry.
func TestRecoverBusyDoesNotMutate(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(root, "created.txt")
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("partial"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	lock := acquireForTest(t, root)

	if _, err := RecoverContext(context.Background(), root, false); !errors.Is(err, ErrBusy) {
		t.Fatalf("RecoverContext while locked error = %v, want ErrBusy", err)
	}
	if _, err := RecoverContext(context.Background(), root, true); !errors.Is(err, ErrBusy) {
		t.Fatalf("RecoverContext(discard) while locked error = %v, want ErrBusy", err)
	}
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("busy recovery removed the created path: %v", err)
	}
	if _, err := os.Stat(tx.tempDir); err != nil {
		t.Fatalf("busy recovery removed the journal: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}

	recovered, err := RecoverContext(context.Background(), root, false)
	if err != nil {
		t.Fatalf("RecoverContext after release failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("RecoverContext handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("recovery left created path %s (err=%v)", created, err)
	}
	assertNotExist(t, filepath.Join(root, fileutil.InternalDir))

	// The lock must be free after recovery.
	probe := acquireForTest(t, root)
	if err := probe.Release(); err != nil {
		t.Fatalf("Release() of post-recovery probe failed: %v", err)
	}
}

// TestRecoverInterruptedNewRemovesRootWhileLocked verifies root removal is
// serialized and leaves no lock or staging residue.
//
//nolint:cyclop // Busy refusal, release, recovery, and staging assertions.
func TestRecoverInterruptedNewRemovesRootWhileLocked(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "demo")
	if err := os.Mkdir(root, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	tx, err := NewWithOptions(root, Options{RemoveRootOnRecovery: true})
	if err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(root, "partial.txt")
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("partial"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	lock := acquireForTest(t, root)
	if _, err := RecoverContext(context.Background(), root, false); !errors.Is(err, ErrBusy) {
		t.Fatalf("RecoverContext while locked error = %v, want ErrBusy", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("busy recovery removed the bootstrap root: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}

	recovered, err := RecoverContext(context.Background(), root, false)
	if err != nil {
		t.Fatalf("RecoverContext failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("RecoverContext handled %d transactions, want 1", recovered)
	}
	assertNotExist(t, root)

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".scbake-remove-") {
			t.Errorf("recovery left staging directory %s", entry.Name())
		}
	}
}

// TestRemoveBootstrappedRootReleasesBeforeDeletingStaging verifies the release
// callback runs after the rename but before the staged tree is deleted, which
// is what lets Windows remove the still-open lock file.
//
//nolint:cyclop // Sets up a root, inspects staging from the callback, then asserts cleanup.
func TestRemoveBootstrappedRootReleasesBeforeDeletingStaging(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "demo")
	if err := os.Mkdir(root, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "partial.txt"), []byte("partial"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	released := false
	err := removeBootstrappedRoot(context.Background(), root, func() error {
		released = true
		entries, readErr := os.ReadDir(parent)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".scbake-remove-") {
				return nil
			}
		}
		return errors.New("staging directory was deleted before release")
	})
	if err != nil {
		t.Fatalf("removeBootstrappedRoot failed: %v", err)
	}
	if !released {
		t.Fatal("removeBootstrappedRoot did not call release")
	}
	assertNotExist(t, root)

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".scbake-remove-") {
			t.Errorf("removeBootstrappedRoot left staging directory %s", entry.Name())
		}
	}
}
