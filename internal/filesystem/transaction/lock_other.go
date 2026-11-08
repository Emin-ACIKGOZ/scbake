// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin && !windows

package transaction

import (
	"errors"
	"os"
)

// errLockUnsupported prevents unsupported platforms from silently running
// mutating commands without cross-process serialization.
var errLockUnsupported = errors.New("project locking is not supported on this platform")

func acquireLockFile(_, _ string) (*os.File, error) {
	return nil, errLockUnsupported
}

func closeLockFile(_ *os.File) error {
	return errLockUnsupported
}

func releaseLockFile(_ *os.File, _ string) error {
	return errLockUnsupported
}
