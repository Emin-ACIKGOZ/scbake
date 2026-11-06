// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package transaction

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// processStartTokensAvailable reports whether processStartToken returns a
// comparable incarnation token. Only Linux exposes /proc/<pid>/stat here.
const processStartTokensAvailable = runtime.GOOS == "linux"

// processAlive reports whether pid currently exists and, when ownerStart is
// available, whether it is the same process incarnation. The start-time check
// prevents a recycled PID from blocking recovery indefinitely.
func processAlive(pid int, ownerStart string) bool {
	if pid <= 0 {
		return false
	}
	if ownerStart != "" {
		current := processStartToken(pid)
		if current == "" || current != ownerStart {
			return false
		}
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// processStartToken returns the process start time from /proc on Linux. Other
// Unix systems return an empty token and fall back to the PID-only check.
func processStartToken(pid int) string {
	if pid <= 0 {
		return ""
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	// The comm field is parenthesized and may contain spaces or parentheses,
	// so split after the final ')'.
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+2 > len(data) {
		return ""
	}
	fields := strings.Fields(string(data[end+1:]))
	// Field 3 (state) is fields[0], so field 22 (starttime) is fields[19].
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return ""
	}
	return fields[startTimeIndex]
}
