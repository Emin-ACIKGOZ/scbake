// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package transaction

import (
	"os"
	"strconv"
	"syscall"
)

// currentUserToken identifies the current user in the private marker directory
// name so users sharing a system temp directory never share markers.
func currentUserToken() string {
	return strconv.Itoa(os.Getuid())
}

// markerOwnerTrusted reports whether the file belongs to the current user.
func markerOwnerTrusted(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(st.Uid) == os.Getuid()
}

// markerModePrivate reports whether the file is inaccessible to group and
// other users.
func markerModePrivate(info os.FileInfo) bool {
	return info.Mode().Perm()&0o077 == 0
}

// markerDirTrusted reports whether info describes a private directory owned by
// the current user.
func markerDirTrusted(info os.FileInfo) bool {
	return info.IsDir() &&
		info.Mode()&os.ModeSymlink == 0 &&
		markerOwnerTrusted(info) &&
		markerModePrivate(info)
}

// markerFileTrusted reports whether info describes a regular, private marker
// file owned by the current user.
func markerFileTrusted(info os.FileInfo) bool {
	return info.Mode().IsRegular() &&
		markerOwnerTrusted(info) &&
		markerModePrivate(info)
}
