// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package fileutil

const (
	// ManifestFileName is the primary configuration file.
	ManifestFileName = "scbake.toml"

	// InternalDir is the hidden state directory.
	InternalDir = ".scbake"

	// TmpDir is the subdirectory for transactional backups.
	TmpDir = "tmp"

	// BaseDir is the subdirectory that stores the pristine rendered content of
	// managed files. It is the common ancestor used by `update` three-way merges.
	BaseDir = "base"

	// GitDir is the standard Git repository directory marker.
	GitDir = ".git"

	// GitIgnore is the standard Git ignore file marker.
	GitIgnore = ".gitignore"

	// ExitSuccess indicates a successful process completion.
	ExitSuccess = 0

	// ExitError indicates a general process failure.
	ExitError = 1
)
