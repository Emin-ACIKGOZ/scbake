// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package fileutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxSymlinkDepth bounds the recursive symlink resolution performed by
// canonicalize, mirroring the kernel's ELOOP protection against link cycles.
const maxSymlinkDepth = 255

// SafePath resolves targetPath against baseDir and verifies that the resulting
// absolute path remains strictly contained inside baseDir.
//
// Relative target paths are joined to baseDir; absolute target paths are checked
// directly against baseDir containment. Absoluteness is detected consistently
// across platforms: a leading "/" or "\" is treated as absolute even on systems
// (such as Windows) where filepath.IsAbs does not recognize it, so foreign-style
// absolute paths are never silently joined under baseDir and can instead be
// rejected by the containment check.
//
// Containment is enforced in two stages. First a lexical check (filepath.Rel on
// cleaned strings) rejects "..", foreign absolute paths and sibling-prefix
// tricks. Then a symlink-aware check resolves the real base and the real target
// (following existing symlinks and tolerating a not-yet-existing tail) and
// requires the resolved target to stay inside the resolved base. A symlink that
// resolves back inside the base is transparent and accepted; any symlink that
// escapes the base is rejected.
//
// Residual race (SCB-002): the path is validated as a string and re-resolved by
// the operating system when the caller later opens it. A sufficiently fast
// attacker who can replace a component between the check and the use can still
// redirect the operation. Callers that write must additionally open the final
// component with O_NOFOLLOW (see SafeOpenFile/SafeWriteFile), which rejects a
// symlinked leaf and narrows the window, but fully closing it requires
// directory-fd relative operations (openat2/os.Root), which are not available
// here. The validated base root is therefore a trusted input.
func SafePath(baseDir, targetPath string) (string, error) {
	if baseDir == "" {
		return "", errors.New("base directory cannot be empty")
	}

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid base directory %q: %w", baseDir, err)
	}
	baseAbs = filepath.Clean(baseAbs)

	absPath := filepath.Clean(targetPath)
	if !filepath.IsAbs(targetPath) && !strings.HasPrefix(targetPath, "/") && !strings.HasPrefix(targetPath, `\`) {
		absPath = filepath.Clean(filepath.Join(baseAbs, targetPath))
	}

	rel, err := filepath.Rel(baseAbs, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal rejected: %q escapes base directory %q", targetPath, baseDir)
	}

	if err := verifyResolvedContainment(baseAbs, absPath); err != nil {
		return "", err
	}

	return absPath, nil
}

// verifyResolvedContainment canonicalizes baseAbs and absPath and rejects the
// target when the resolved target escapes the resolved base.
func verifyResolvedContainment(baseAbs, absPath string) error {
	resolvedBase, err := canonicalize(baseAbs, 0)
	if err != nil {
		return fmt.Errorf("failed to resolve base directory %q: %w", baseAbs, err)
	}

	resolvedTarget, err := canonicalize(absPath, 0)
	if err != nil {
		return fmt.Errorf("failed to resolve path %q: %w", absPath, err)
	}

	rel, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil {
		return fmt.Errorf("failed to compute relative path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf(
			"path traversal rejected: %q resolves outside base directory %q via symlink",
			absPath, baseAbs,
		)
	}

	return nil
}

// canonicalize returns the absolute, symlink-resolved form of p. Unlike
// filepath.EvalSymlinks it tolerates a trailing path that does not exist yet
// (common for files about to be created): the deepest existing ancestor is
// resolved and the remaining components are appended lexically. A dangling
// symlink is still resolved through os.Readlink, so an escaping link is never
// mistaken for a non-existent path and silently accepted.
func canonicalize(p string, depth int) (string, error) {
	if depth > maxSymlinkDepth {
		return "", errors.New("too many levels of symbolic links")
	}
	p = filepath.Clean(p)

	// Find the deepest component that exists, using Lstat so a symlink (even a
	// dangling one) counts as existing rather than being skipped over.
	existing := p
	var tail []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", &os.PathError{Op: "lstat", Path: p, Err: os.ErrNotExist}
		}
		tail = append(tail, filepath.Base(existing))
		existing = parent
	}

	info, err := os.Lstat(existing)
	if err != nil {
		return "", err
	}

	// A symlink as the deepest existing component must be resolved manually:
	// EvalSymlinks would fail on a dangling link, and falling back to the
	// lexical path would let the link escape undetected.
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(existing)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(existing), target)
		}
		for i := len(tail) - 1; i >= 0; i-- {
			target = filepath.Join(target, tail[i])
		}
		return canonicalize(target, depth+1)
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	for i := len(tail) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, tail[i])
	}
	return resolved, nil
}

// openNoFollow opens path with flag and perm, additionally refusing to follow a
// symbolic link as the final path component (O_NOFOLLOW on Unix). The parent
// directory has already been validated by SafePath, and rejecting a symlinked
// leaf narrows the check→use window described on SafePath.
func openNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag|oNoFollow, perm)
}

// writeIntent reports whether flag opens the file for modification.
func writeIntent(flag int) bool {
	return flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
}

// refuseHardlinkedTarget rejects a write to a file with more than one hard link
// (SCB-003): such a file shares its inode with a path that may live outside the
// base directory, so truncating or overwriting it would modify data outside the
// base. The check is best-effort and inherently racy (the link count can change
// between this check and the open); on platforms that cannot report a link
// count it is a no-op.
func refuseHardlinkedTarget(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return nil // Non-existent target: the subsequent open handles it.
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil // openNoFollow rejects symlinks.
	}
	if nlink, ok := hardlinkCount(info); ok && nlink > 1 {
		return fmt.Errorf(
			"refusing to write %q: file has %d hard links and may be shared outside the base directory",
			path, nlink,
		)
	}
	return nil
}

// SafeReadFile validates targetPath containment inside baseDir then reads the
// file without following a symlinked final component.
func SafeReadFile(baseDir, targetPath string) ([]byte, error) {
	safePath, err := SafePath(baseDir, targetPath)
	if err != nil {
		return nil, err
	}

	f, err := openNoFollow(safePath, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return io.ReadAll(f)
}

// SafeOpenFile validates targetPath containment inside baseDir then opens the
// file without following a symlinked final component. Writes additionally
// refuse a hard-linked target (SCB-003).
func SafeOpenFile(baseDir, targetPath string, flag int, perm os.FileMode) (*os.File, error) {
	safePath, err := SafePath(baseDir, targetPath)
	if err != nil {
		return nil, err
	}

	if writeIntent(flag) {
		if err := refuseHardlinkedTarget(safePath); err != nil {
			return nil, err
		}
	}

	return openNoFollow(safePath, flag, perm)
}

// SafeWriteFile validates targetPath containment inside baseDir then writes the
// file without following a symlinked final component. A hard-linked target is
// refused (SCB-003).
func SafeWriteFile(baseDir, targetPath string, data []byte, perm os.FileMode) error {
	safePath, err := SafePath(baseDir, targetPath)
	if err != nil {
		return err
	}

	if err := refuseHardlinkedTarget(safePath); err != nil {
		return err
	}

	f, err := openNoFollow(safePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}

	return f.Close()
}
