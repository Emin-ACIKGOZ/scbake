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
// createRoot controls whether a missing project root may be created. `new`
// passes true because it bootstraps the root; commands operating on an existing
// project pass false so a bad path does not leave an empty directory behind.
// It is variadic to keep the original single-argument form as a safe default
// (no root creation).
//
// The lock file is unlinked on release rather than kept around, preserving
// existing cleanup behavior. A process preempted between opening and locking
// could otherwise lock an unlinked inode while another process locks a fresh
// one at the same path; the post-acquire SameFile check discards a handle whose
// path no longer names it and retries.
func AcquireRootLock(root string, createRoot ...bool) (*RootLock, error) {
	allowCreate := len(createRoot) > 0 && createRoot[0]
	paths, err := resolveRootLockPaths(root, allowCreate)
	if err != nil {
		return nil, err
	}

	for attempt := 0; attempt < maxRootLockAttempts; attempt++ {
		lock, retry, err := tryAcquireRootLock(paths)
		if err != nil {
			return nil, err
		}
		if !retry {
			return lock, nil
		}
	}
	return nil, fmt.Errorf("failed to acquire project lock %s: lock path kept changing", paths.lockPath)
}

// rootLockPaths holds the canonical root and its lock locations.
type rootLockPaths struct {
	root       string
	stateDir   string
	lockPath   string
	createRoot bool
}

// resolveRootLockPaths canonicalizes the root and derives its lock locations.
// It must not resolve the state directory or lock file yet: .scbake is created
// lazily and a concurrent release may prune it between resolution and open.
// Containment is validated after the state directory exists in
// ensureRootLockDir.
func resolveRootLockPaths(root string, createRoot bool) (rootLockPaths, error) {
	absRoot, err := CanonicalRoot(root)
	if err != nil {
		return rootLockPaths{}, fmt.Errorf("failed to resolve root path: %w", err)
	}
	stateDir := filepath.Join(absRoot, fileutil.InternalDir)
	lockPath := filepath.Join(stateDir, fileutil.LockFileName)
	return rootLockPaths{
		root:       absRoot,
		stateDir:   stateDir,
		lockPath:   lockPath,
		createRoot: createRoot,
	}, nil
}

// tryAcquireRootLock makes one acquisition attempt. retry reports that .scbake
// was pruned or the lock path was replaced, so the caller should reopen it.
func tryAcquireRootLock(paths rootLockPaths) (lock *RootLock, retry bool, err error) {
	if err := ensureRootLockDir(paths); err != nil {
		// A releasing process may have pruned .scbake; recreate and retry.
		if errors.Is(err, fs.ErrNotExist) && paths.createRoot {
			return nil, true, nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, fmt.Errorf("project root %s does not exist: %w", paths.root, fs.ErrNotExist)
		}
		return nil, false, fmt.Errorf("failed to create lock directory %s: %w", paths.stateDir, err)
	}

	f, err := acquireLockFile(paths.root, paths.lockPath)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			return nil, false, fmt.Errorf("%w: %s", ErrBusy, paths.root)
		}
		// The same prune race can remove the directory before the open.
		if errors.Is(err, fs.ErrNotExist) {
			return nil, true, nil
		}
		return nil, false, err
	}

	if lockPathMatches(f, paths.lockPath) {
		return &RootLock{file: f, path: paths.lockPath, root: paths.root}, false, nil
	}

	// The path was unlinked or replaced between open and lock: drop the stale
	// handle and retry from open.
	_ = closeLockFile(f)
	return nil, true, nil
}

// ensureRootLockDir creates .scbake. For existing-project commands it uses
// os.Mkdir rather than MkdirAll so a missing root is reported instead of being
// silently created as an empty directory.
func ensureRootLockDir(paths rootLockPaths) error {
	if paths.createRoot {
		if err := os.MkdirAll(paths.stateDir, rootLockDirPerm); err != nil {
			return err
		}
	} else {
		err := os.Mkdir(paths.stateDir, rootLockDirPerm)
		if err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
	}
	if _, err := fileutil.SafePath(paths.root, paths.stateDir); err != nil {
		return fmt.Errorf("invalid lock directory: %w", err)
	}
	return nil
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
