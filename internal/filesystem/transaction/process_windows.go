// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package transaction

import (
	"errors"
	"syscall"
)

const (
	processQueryLimitedInformation = 0x1000
	stillActiveExitCode            = 259

	// processStartTokensAvailable is false because Windows does not expose a
	// portable start-time token here; liveness is bounded by age instead.
	processStartTokensAvailable = false
)

// processAlive reports whether pid currently exists. Windows does not expose a
// portable start-time token here, so liveness falls back to the process handle.
func processAlive(pid int, _ string) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// Access denied means the process exists but cannot be queried; a
		// missing process reports an invalid parameter. Treat the former as
		// alive so recovery never rolls back a running transaction, and the
		// latter as dead so a stale PID does not block forever.
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = syscall.CloseHandle(handle) }()

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		// The handle was valid, so the process exists even if its exit code is
		// unavailable. Stay conservative rather than treating it as dead.
		return true
	}
	return exitCode == stillActiveExitCode
}

// processStartToken is unavailable on Windows; age-bounded PID liveness is used.
func processStartToken(int) string {
	return ""
}
