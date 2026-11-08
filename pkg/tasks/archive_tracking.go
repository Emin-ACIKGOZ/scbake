// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"archive/zip"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// trackArchiveEntries registers every destination of a zip archive with the
// transaction before an extraction command runs. This closes the gap where a
// declared top-level directory already exists: tracking the directory is a
// no-op, but individual nested entries can still be overwritten or created.
//
// The archive path is resolved against cmdDir (the command's working
// directory) and must stay inside the transaction root. Every entry name is
// validated before any path is tracked, so a rejected entry prevents the
// extraction command from running at all.
func trackArchiveEntries(tc types.TaskContext, cmdDir, archivePath string) error {
	if tc.Tx == nil || tc.DryRun {
		return nil
	}

	root := taskRoot(tc)
	if root == "" {
		return errors.New("cannot track archive entries without a project root")
	}

	absArchive, err := resolveArchivePath(root, cmdDir, archivePath)
	if err != nil {
		return err
	}

	zr, err := zip.OpenReader(absArchive)
	if err != nil {
		return fmt.Errorf("failed to open archive %s: %w", absArchive, err)
	}
	defer func() { _ = zr.Close() }()

	destinations, err := archiveDestinations(root, cmdDir, absArchive, zr.File)
	if err != nil {
		return err
	}
	return trackArchiveDestinations(tc, destinations)
}

// archiveDestination is one validated extraction target.
type archiveDestination struct {
	name string
	path string
}

// resolveArchivePath resolves a possibly relative archive path against the
// command's working directory and validates it against the transaction root.
func resolveArchivePath(root, cmdDir, archivePath string) (string, error) {
	absArchive := archivePath
	if !filepath.IsAbs(absArchive) {
		absArchive = filepath.Join(cmdDir, archivePath)
	}
	absArchive, err := fileutil.SafePath(root, absArchive)
	if err != nil {
		return "", fmt.Errorf("archive path %q resolves outside the project root: %w", archivePath, err)
	}
	return absArchive, nil
}

// archiveDestinations validates every archive entry and resolves its
// destination before any path is tracked. A single unsafe entry rejects the
// whole archive, so the extraction command never starts.
func archiveDestinations(root, cmdDir, absArchive string, files []*zip.File) ([]archiveDestination, error) {
	absCmdDir, err := filepath.Abs(cmdDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve command directory %s: %w", cmdDir, err)
	}

	destinations := make([]archiveDestination, 0, len(files))
	for _, entry := range files {
		rel, err := safeArchiveEntryName(entry.Name)
		if err != nil {
			return nil, fmt.Errorf("refusing archive entry %q in %s: %w", entry.Name, absArchive, err)
		}

		dest := filepath.Join(absCmdDir, rel)
		if _, err := fileutil.SafePath(root, dest); err != nil {
			return nil, fmt.Errorf("archive entry %q resolves outside the project root: %w", entry.Name, err)
		}
		destinations = append(destinations, archiveDestination{name: entry.Name, path: dest})
	}
	return destinations, nil
}

// trackArchiveDestinations registers each validated destination and its
// missing ancestors with the transaction.
func trackArchiveDestinations(tc types.TaskContext, destinations []archiveDestination) error {
	for _, dest := range destinations {
		if err := tc.Tx.Track(dest.path); err != nil {
			return fmt.Errorf("failed to track archive entry %q (%s): %w", dest.name, dest.path, err)
		}
		if err := trackMissingAncestors(tc, dest.path); err != nil {
			return fmt.Errorf("failed to track archive entry %q ancestors: %w", dest.name, err)
		}
	}
	return nil
}

// safeArchiveEntryName validates a zip entry name and returns it as a relative
// OS path. Zip entries are specified with forward slashes; backslashes are
// rejected because their interpretation as separators differs across
// platforms. Absolute paths, traversal outside the extraction root, and
// empty or "." names are also rejected.
func safeArchiveEntryName(name string) (string, error) {
	if strings.Contains(name, `\`) {
		return "", errors.New("backslash in entry name")
	}

	// Normalize before validating absolute and drive-prefixed forms. Checking
	// only the raw name lets "./A:" and "./C:/escape.txt" through: they clean
	// to "A:" and "C:/escape.txt", which are drive-relative on Windows.
	clean := path.Clean(name)
	if err := rejectArchiveEntryTraversal(name, clean); err != nil {
		return "", err
	}
	if err := rejectArchiveEntryAbsolutePath(name); err != nil {
		return "", err
	}
	if err := rejectArchiveEntryAbsolutePath(clean); err != nil {
		return "", err
	}

	return filepath.FromSlash(clean), nil
}

// rejectArchiveEntryTraversal rejects empty, current-directory, parent, and
// escaping names. Parent components are rejected in the raw form as well so a
// path such as "nested/../../escape.txt" cannot normalize into an accepted
// name before validation.
func rejectArchiveEntryTraversal(name, clean string) error {
	if clean == "." {
		return errors.New("empty or '.' entry name")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return errors.New("entry path contains '..' traversal")
		}
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("entry path traverses outside the extraction root")
	}
	return nil
}

// rejectArchiveEntryAbsolutePath rejects rooted, volume-prefixed, and Windows
// drive-relative names. It is called on both the raw and normalized forms.
func rejectArchiveEntryAbsolutePath(name string) error {
	if strings.HasPrefix(name, "/") || path.IsAbs(name) || filepath.IsAbs(name) ||
		filepath.VolumeName(name) != "" || isWindowsDrivePath(name) {
		return errors.New("absolute entry path")
	}
	return nil
}

// isWindowsDrivePath reports whether name starts with a Windows drive prefix
// such as "C:". It is checked on every platform so a foreign-style absolute
// path cannot slip through and be interpreted by a Windows extractor.
func isWindowsDrivePath(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	c := name[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
