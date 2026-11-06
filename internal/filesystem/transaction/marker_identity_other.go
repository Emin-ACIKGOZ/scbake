// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package transaction

import "os"

// currentUserToken identifies the marker directory. On these platforms the
// system temporary directory is already per-user and has no POSIX uid to name.
func currentUserToken() string {
	return "user"
}

// markerOwnerTrusted cannot check a POSIX owner on this platform; the private
// per-user temporary directory provides the isolation instead.
func markerOwnerTrusted(os.FileInfo) bool {
	return true
}

// markerModePrivate cannot enforce POSIX permission bits on this platform.
func markerModePrivate(os.FileInfo) bool {
	return true
}

// markerDirTrusted reports whether info describes a directory, not a symlink.
func markerDirTrusted(info os.FileInfo) bool {
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

// markerFileTrusted reports whether info describes a regular marker file.
func markerFileTrusted(info os.FileInfo) bool {
	return info.Mode().IsRegular()
}
