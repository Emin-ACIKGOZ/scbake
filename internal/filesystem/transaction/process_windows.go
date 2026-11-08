// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package transaction

import "syscall"

const (
	processQueryLimitedInformation = 0x1000
	stillActiveExitCode            = 259
)

// processAlive reports whether pid currently exists. Windows does not expose a
// portable start-time token here, so liveness falls back to the process handle.
func processAlive(pid int, _ string) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(handle) }()

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == stillActiveExitCode
}

// processStartToken is unavailable on Windows; PID-only liveness is used.
func processStartToken(int) string {
	return ""
}
