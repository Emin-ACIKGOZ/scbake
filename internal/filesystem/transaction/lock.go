// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
)

const (
	maxRootLockAttempts = 3
	rootLockDirPerm     = os.FileMode(0o700)
)

// ErrBusy reports that another scbake process holds the project's root lock.
// Callers may retry after that process finishes.
var ErrBusy = errors.New("another scbake process is operating on this project; retry after it finishes")

// RootLock is an exclusive advisory lock on a project root, held for the whole
// mutation critical section: manifest load through commit or rollback.
type RootLock struct {
	file *os.File
	path string
	root string
}

// AcquireRootLock acquires a non-blocking exclusive lock on
// <root>/.scbake/lock. It creates .scbake with owner-only permissions when
// missing; only callers that intend to mutate the project should call it.
//
// The lock file is unlinked on release rather than kept around, preserving
// existing cleanup behavior. A process preempted between opening and locking
// could otherwise lock an unlinked inode while another process locks a fresh
// one at the same path; the post-acquire SameFile check discards a handle whose
// path no longer names it and retries.
func AcquireRootLock(root string) (*RootLock, error) {
	absRoot, err := CanonicalRoot(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve root path: %w", err)
	}
	stateDir := filepath.Join(absRoot, fileutil.InternalDir)
	lockPath := filepath.Join(stateDir, fileutil.LockFileName)
	if _, err := fileutil.SafePath(absRoot, stateDir); err != nil {
		return nil, fmt.Errorf("invalid lock directory: %w", err)
	}
	if _, err := fileutil.SafePath(absRoot, lockPath); err != nil {
		return nil, fmt.Errorf("invalid lock path: %w", err)
	}

	for attempt := 0; attempt < maxRootLockAttempts; attempt++ {
		if err := os.MkdirAll(stateDir, rootLockDirPerm); err != nil {
			// A releasing process may have pruned .scbake; recreate and retry.
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("failed to create lock directory %s: %w", stateDir, err)
		}
		f, err := acquireLockFile(absRoot, lockPath)
		if err != nil {
			if errors.Is(err, ErrBusy) {
				return nil, fmt.Errorf("%w: %s", ErrBusy, absRoot)
			}
			// The same prune race can remove the directory before the open.
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if lockPathMatches(f, lockPath) {
			return &RootLock{file: f, path: lockPath, root: absRoot}, nil
		}
		// The path was unlinked or replaced between open and lock: drop the
		// stale handle and retry from open.
		_ = closeLockFile(f)
	}
	return nil, fmt.Errorf("failed to acquire project lock %s: lock path kept changing", lockPath)
}

// Release removes the lock file while the lock is still held, then unlocks and
// closes it. Removing first is deliberate: unlocking first would let a
// preempted opener acquire the old inode and validate it before the unlink,
// allowing a third process to create and lock a new inode at the same path.
// Release is idempotent.
func (l *RootLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	f, path := l.file, l.path
	l.file = nil
	err := releaseLockFile(f, path)

	// The state directory is shared with the journal, so this only succeeds
	// when it is empty. Best effort: the next mutating run reuses it.
	_ = os.Remove(filepath.Join(l.root, fileutil.InternalDir))
	return err
}

// lockPathMatches reports whether the open handle and path still name the same
// file. Lstat is used so a symlink leaf is never accepted as the locked file.
func lockPathMatches(f *os.File, path string) bool {
	locked, err := f.Stat()
	if err != nil {
		return false
	}
	atPath, err := os.Lstat(path)
	return err == nil && os.SameFile(locked, atPath)
}
