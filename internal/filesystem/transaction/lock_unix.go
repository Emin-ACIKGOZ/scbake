// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package transaction

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"scbake/internal/util/fileutil"
	"syscall"
)

// acquireLockFile opens the lock path without following a symlinked leaf and
// takes a non-blocking exclusive flock, retrying on EINTR.
func acquireLockFile(root, path string) (*os.File, error) {
	f, err := fileutil.SafeOpenFile(root, path, os.O_CREATE|os.O_RDWR, fileutil.PrivateFilePerms)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return f, nil
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EAGAIN):
			_ = f.Close()
			return nil, ErrBusy
		default:
			_ = f.Close()
			return nil, err
		}
	}
}

// closeLockFile unlocks and closes without touching the path.
func closeLockFile(f *os.File) error {
	unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if unlockErr != nil {
		unlockErr = fmt.Errorf("failed to unlock %s: %w", f.Name(), unlockErr)
	}
	return errors.Join(unlockErr, f.Close())
}

// releaseLockFile unlinks the path while still holding the lock, then unlocks
// and closes. A path that no longer names this lock (for example after the root
// was renamed during recovery) is left alone.
func releaseLockFile(f *os.File, path string) error {
	var removeErr error
	if lockPathMatches(f, path) {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			removeErr = fmt.Errorf("failed to remove lock file %s: %w", path, err)
		}
	}
	return errors.Join(removeErr, closeLockFile(f))
}
