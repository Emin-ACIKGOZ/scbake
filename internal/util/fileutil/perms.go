// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package fileutil

import "os"

const (
	// DirPerms (0750) is the secure setting for directories (rwxr-x---).
	DirPerms os.FileMode = 0o750

	// FilePerms (0644) is the standard setting for public project files (rw-r--r--).
	FilePerms os.FileMode = 0o644

	// PrivateFilePerms (0600) is for sensitive files like the manifest (rw-------).
	PrivateFilePerms os.FileMode = 0o600
)
