// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"scbake/internal/util/fileutil"
)

// validateCommittedBaseTree verifies that the committed base root
// <root>/.scbake-base and every existing directory between it and the target
// file for ledgerKey are real directories. A symlinked or non-directory
// component is rejected even when it resolves back inside the project root:
// SafePath accepts an in-root symlink, but the committed pristine base must
// never be redirected into an unrelated source directory.
//
// A missing base tree is allowed because the first write creates it. The error
// names the offending path and explains why a symlinked base is refused.
//
// The Lstat guard is check-then-use: a concurrent replacement of a validated
// component can still redirect a later open. This is the same documented
// residual race as SafePath (SCB-002); fully closing it requires directory-fd
// relative operations, which are not available here.
func validateCommittedBaseTree(root, ledgerKey string) error {
	dirs, err := committedBaseDirs(root, ledgerKey)
	if err != nil {
		return err
	}
	return rejectNonRealBaseDirs(dirs)
}

// ValidateBaseShadow verifies that the committed base tree for ledgerKey is made
// of real directories. It performs only Lstat checks and never reads or writes
// the base file, so callers that accept, unmanage, or otherwise mutate base
// state can use it as a preflight. A missing base tree is not an error; a
// symlinked or non-directory component is rejected with a clear error.
//
// Like SafePath, the check is check-then-use and retains the documented residual
// race with a concurrent filesystem mutation.
func ValidateBaseShadow(root, ledgerKey string) error {
	return validateCommittedBaseTree(root, ledgerKey)
}

// committedBaseDirs returns the committed base root and every directory between
// it and the target file for ledgerKey, excluding the base file leaf. A missing
// base tree is not an error: the first write creates it. The error names the
// ledger key whose base path could not be resolved inside the committed base.
func committedBaseDirs(root, ledgerKey string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("invalid project root %q: %w", root, err)
	}

	dirs := []string{filepath.Join(rootAbs, fileutil.CommittedBaseDir)}
	if ledgerKey == "" {
		return dirs, nil
	}

	absPath, err := fileutil.SafePath(rootAbs, BaseShadowRelPath(ledgerKey))
	if err != nil {
		return nil, fmt.Errorf("invalid base shadow path for %s: %w", ledgerKey, err)
	}
	rel, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		return nil, fmt.Errorf("invalid base shadow path for %s: %w", ledgerKey, err)
	}
	components := strings.Split(filepath.ToSlash(rel), "/")
	if len(components) == 0 || components[0] != fileutil.CommittedBaseDir {
		return nil, fmt.Errorf(
			"invalid base shadow path for %s: not inside %s", ledgerKey, fileutil.CommittedBaseDir,
		)
	}
	// Skip the final component: it is the base file, whose leaf symlink is
	// already refused by SafeOpenFile/SafeReadFile.
	dir := rootAbs
	for _, component := range components[:len(components)-1] {
		dir = filepath.Join(dir, component)
		if dir != dirs[0] {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

// rejectNonRealBaseDirs Lstats each directory in order and rejects a symlinked
// or non-directory component. A missing directory ends the walk successfully
// because a missing base tree is the first-run case; everything beneath a
// missing component is necessarily missing too.
func rejectNonRealBaseDirs(dirs []string) error {
	for _, dir := range dirs {
		info, err := os.Lstat(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil // A missing base tree is the first-run case.
			}
			return fmt.Errorf("failed to inspect committed base directory %s: %w", dir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf(
				"committed base directory %s is a symbolic link; a symlinked base directory is not allowed: "+
					"the committed pristine base must be a real directory tree",
				dir,
			)
		}
		if !info.IsDir() {
			return fmt.Errorf(
				"committed base path %s is not a directory; the committed pristine base must be a real directory tree",
				dir,
			)
		}
	}
	return nil
}

// ReadBaseShadow returns the pristine base content for a ledger key after
// validating that the committed base tree is made of real directories. A
// missing base root or file is reported as fs.ErrNotExist, while a redirected
// base tree (symlink or non-directory component) is a distinct error callers
// must surface rather than treat as a missing base.
func ReadBaseShadow(root, ledgerKey string) ([]byte, error) {
	if root == "" {
		return nil, errors.New("base root cannot be empty")
	}
	if ledgerKey == "" {
		return nil, errors.New("base ledger key cannot be empty")
	}
	if err := validateCommittedBaseTree(root, ledgerKey); err != nil {
		return nil, err
	}
	return fileutil.SafeReadFile(root, BaseShadowRelPath(ledgerKey))
}
