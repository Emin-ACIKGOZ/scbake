// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package fileutil

import (
	"os"
	"syscall"
)

// oNoFollow is the O_NOFOLLOW open flag. When set, opening a path whose final
// component is a symbolic link fails with ELOOP instead of following the link.
const oNoFollow = syscall.O_NOFOLLOW

// hardlinkCount returns the number of hard links referring to the file described
// by info. ok is false when the platform cannot report it.
func hardlinkCount(info os.FileInfo) (uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Nlink), true
}
