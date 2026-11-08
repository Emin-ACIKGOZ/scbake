// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package transaction

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockFileAccess    = syscall.GENERIC_READ | syscall.GENERIC_WRITE
	lockFileShareMode = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE

	lockFileExclusiveLock   = 0x00000002
	lockFileFailImmediately = 0x00000001

	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

// acquireLockFile opens the lock file with share-delete so it can be removed
// while held and so root removal is not blocked, then locks one byte at offset
// zero without waiting.
func acquireLockFile(_, path string) (*os.File, error) {
	utf16Path, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("failed to encode lock path %s: %w", path, err)
	}
	handle, err := syscall.CreateFile(
		utf16Path, lockFileAccess, lockFileShareMode, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0,
	)
	if err != nil {
		if isLockBusyError(err) {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("failed to open lock file %s: %w", path, err)
	}
	f := os.NewFile(uintptr(handle), path)

	if err := lockFileEx(f); err != nil {
		_ = f.Close()
		if isLockBusyError(err) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return f, nil
}

// lockFileEx takes a non-blocking exclusive lock on one byte at offset zero.
func lockFileEx(f *os.File) error {
	if err := procLockFileEx.Find(); err != nil {
		return fmt.Errorf("LockFileEx unavailable: %w", err)
	}
	var overlapped syscall.Overlapped
	result, _, callErr := procLockFileEx.Call(
		f.Fd(), uintptr(lockFileExclusiveLock|lockFileFailImmediately), 0, 1, 0,
		uintptr(unsafe.Pointer(&overlapped)), //nolint:gosec // LockFileEx requires an OVERLAPPED pointer.
	)
	if result != 0 {
		return nil
	}
	if callErr == syscall.Errno(0) {
		callErr = errors.New("LockFileEx failed")
	}
	return callErr
}

// closeLockFile unlocks and closes without touching the path.
func closeLockFile(f *os.File) error {
	if err := procUnlockFileEx.Find(); err != nil {
		return errors.Join(fmt.Errorf("UnlockFileEx unavailable: %w", err), f.Close())
	}
	var overlapped syscall.Overlapped
	result, _, callErr := procUnlockFileEx.Call(
		f.Fd(), 0, 1, 0,
		uintptr(unsafe.Pointer(&overlapped)), //nolint:gosec // UnlockFileEx requires an OVERLAPPED pointer.
	)
	if result != 0 {
		callErr = nil
	} else if callErr == syscall.Errno(0) {
		callErr = errors.New("UnlockFileEx failed")
	}
	return errors.Join(callErr, f.Close())
}

// releaseLockFile unlocks and closes first, then removes the path, so the lock
// is never left held if removal fails. The identity check runs while the handle
// is still open, so a replaced path is not removed.
func releaseLockFile(f *os.File, path string) error {
	matches := lockPathMatches(f, path)
	closeErr := closeLockFile(f)
	var removeErr error
	if matches {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			removeErr = fmt.Errorf("failed to remove lock file %s: %w", path, err)
		}
	}
	return errors.Join(closeErr, removeErr)
}

// isLockBusyError maps contention and transient delete-pending errors to
// ErrBusy. ERROR_ACCESS_DENIED is included because a delete-pending file
// rejects new opens until the last handle closes.
func isLockBusyError(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, errorSharingViolation) ||
		errors.Is(err, errorLockViolation)
}
