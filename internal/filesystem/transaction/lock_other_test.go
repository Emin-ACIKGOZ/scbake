// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin && !windows

package transaction

import (
	"errors"
	"testing"
)

// TestAcquireRootLockUnsupported verifies unsupported platforms fail closed
// instead of silently running mutating commands without serialization.
func TestAcquireRootLockUnsupported(t *testing.T) {
	root := t.TempDir()

	lock, err := AcquireRootLock(root)
	if lock != nil {
		_ = lock.Release()
		t.Fatal("AcquireRootLock returned a lock on an unsupported platform")
	}
	if !errors.Is(err, errLockUnsupported) {
		t.Fatalf("AcquireRootLock error = %v, want errLockUnsupported", err)
	}
}

// TestLockPlatformHelpersUnsupported covers the helper surface directly so the
// build-tagged fallback is compiled and exercised on unsupported platforms.
func TestLockPlatformHelpersUnsupported(t *testing.T) {
	if _, err := acquireLockFile("", ""); !errors.Is(err, errLockUnsupported) {
		t.Errorf("acquireLockFile error = %v, want errLockUnsupported", err)
	}
	if err := closeLockFile(nil); !errors.Is(err, errLockUnsupported) {
		t.Errorf("closeLockFile error = %v, want errLockUnsupported", err)
	}
	if err := releaseLockFile(nil, ""); !errors.Is(err, errLockUnsupported) {
		t.Errorf("releaseLockFile error = %v, want errLockUnsupported", err)
	}
}
