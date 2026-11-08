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

	// LockFileName is the per-root advisory lock file held by mutating runs.
	LockFileName = "lock"

	// BaseDir is the legacy subdirectory, under InternalDir, that stored the
	// pristine rendered content of managed files. New writes use
	// CommittedBaseDir instead; this constant is kept for migration-aware
	// tooling and tests.
	BaseDir = "base"

	// CommittedBaseDir is the root-level directory that stores the pristine
	// rendered content of managed files. It is the common ancestor used by
	// `update` three-way merges and is committed with the project.
	CommittedBaseDir = ".scbake-base"

	// BaseShadowSuffix is appended to Git-special filenames inside the committed
	// base tree so a copied .gitignore or .gitattributes cannot hide or
	// transform other base payloads.
	BaseShadowSuffix = ".scbake-shadow"

	// GitAttributes is the standard Git attributes file marker.
	GitAttributes = ".gitattributes"

	// GitDir is the standard Git repository directory marker.
	GitDir = ".git"

	// GitIgnore is the standard Git ignore file marker.
	GitIgnore = ".gitignore"

	// ExitSuccess indicates a successful process completion.
	ExitSuccess = 0

	// ExitError indicates a general process failure.
	ExitError = 1

	// ExitInterrupted is the conventional exit status after SIGINT (128 + 2).
	ExitInterrupted = 130

	// ExitTerminated is the conventional exit status after SIGTERM (128 + 15).
	ExitTerminated = 143

	// ExitBusy is EX_TEMPFAIL: another scbake process holds the project lock,
	// so the command did not run and the caller may retry later.
	ExitBusy = 75
)
