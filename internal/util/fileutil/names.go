// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package fileutil provides centralized constants for filesystem metadata and permissions.
package fileutil

const (
	// ManifestFileName is the primary configuration file.
	ManifestFileName = "scbake.toml"

	// InternalDir is the hidden state directory.
	InternalDir = ".scbake"

	// TmpDir is the subdirectory for transactional backups.
	TmpDir = "tmp"

	// GitDir is the standard Git repository directory marker.
	GitDir = ".git"

	// GitIgnore is the standard Git ignore file marker.
	GitIgnore = ".gitignore"

	// ExitSuccess indicates a successful process completion.
	ExitSuccess = 0

	// ExitError indicates a general process failure.
	ExitError = 1
)
