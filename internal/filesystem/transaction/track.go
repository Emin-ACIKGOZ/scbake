// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
)

// Track registers a path that is about to be modified or created.
// It creates a backup if the file exists, or records it for deletion if it doesn't.
func (m *Manager) Track(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	absPath, err := m.resolveAndValidate(path)
	if err != nil {
		return err
	}

	// Check if we have already tracked this file to avoid double-backup overhead
	if m.alreadyTracked(absPath) {
		return nil
	}

	info, err := os.Lstat(absPath)
	if os.IsNotExist(err) {
		// Case 1: File/Dir does not exist. Journal it as created before the
		// task writes, so a crash after the write can still remove it.
		return m.trackCreated(absPath)
	}
	if err != nil {
		// Some other error (permission, etc)
		return fmt.Errorf("failed to stat %s: %w", absPath, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return checkTrackedSymlink(absPath)
	}

	// Case 2: File exists.
	if info.IsDir() {
		// Backing up directories is complex (recursive copy).
		// For atomic scaffolding, we usually don't overwrite directories, we merge into them.
		// If a task needs to DELETE a directory, it requires specific handling.
		// For now, if we encounter a directory, we assume we are just adding to it,
		// so we don't back up the *entire folder*, only the specific files inside it that we touch.
		// Therefore, we do nothing here for directories themselves unless we intend to delete them.
		// We trust Track() will be called for the specific files inside.
		return nil
	}

	// Refuse hard-linked files (SCB-003): a file with more than one link shares
	// its inode with a path that may live outside the project root, so backing
	// up or overwriting it would read or modify data outside the root. The
	// check is best-effort and inherently racy; on platforms without link-count
	// reporting it is a no-op.
	if nlink, ok := hardlinkCount(info); ok && nlink > 1 {
		return fmt.Errorf(
			"security violation: refusing to track hard-linked file %s (%d links)",
			absPath, nlink,
		)
	}

	return m.backupFile(absPath, info)
}

// checkTrackedSymlink validates the symlink at absPath. A symlink to a
// directory is transparent (MkdirAll merges into it) and returns nil. A symlink
// to a file returns an error: the write helpers open with O_NOFOLLOW and would
// reject it anyway, and backing up through the link could touch a shared inode.
// SafePath has already confirmed the link resolves inside the root.
func checkTrackedSymlink(absPath string) error {
	target, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat symlink target %s: %w", absPath, err)
	}
	if target.IsDir() {
		return nil
	}
	return fmt.Errorf("security violation: refusing to track symlink %s", absPath)
}

func (m *Manager) resolveAndValidate(path string) (string, error) {
	// Resolve relative paths against the manager's root, not the process
	// working directory, so Track behaves consistently regardless of CWD.
	// Absolute paths are validated against the root directly. fileutil.SafePath
	// performs both the lexical and the symlink-aware containment checks, so a
	// path reached through a symlink that escapes the root is rejected.
	absPath, err := fileutil.SafePath(m.rootPath, path)
	if err != nil {
		return "", fmt.Errorf("security violation: %w", err)
	}
	return absPath, nil
}

func (m *Manager) alreadyTracked(absPath string) bool {
	if _, backedUp := m.backups[absPath]; backedUp {
		return true
	}
	_, found := m.createdSet[absPath]
	return found
}

func (m *Manager) backupFile(absPath string, info os.FileInfo) error {
	if err := m.ensureTempDir(); err != nil {
		return err
	}

	// Create a unique backup name (hash or flat path)
	// We use flat naming replacement to avoid collisions
	safeName := strings.ReplaceAll(filepath.Base(absPath), string(os.PathSeparator), "_")
	backupPath := filepath.Join(
		m.tempDir,
		fmt.Sprintf("%d_%s", len(m.backups), safeName),
	)

	// It's a file. Copy it to temp dir.
	if err := copyFile(absPath, backupPath, info.Mode()); err != nil {
		return fmt.Errorf("failed to backup file %s: %w", absPath, err)
	}

	// Journal the backup before the caller overwrites the original. If the
	// journal record is not durable, remove the now-unreferenced backup and
	// abort. A close error after a durable record must keep the backup: both
	// rollback and crash recovery need it.
	journalPath, err := m.journalPathFor(absPath)
	if err != nil {
		_ = os.Remove(backupPath)
		return err
	}
	durable, err := m.appendJournal(journalRecord{
		Op:     journalOpBackup,
		Path:   journalPath,
		Backup: filepath.Base(backupPath),
		Mode:   uint32(info.Mode()),
	})
	if err != nil && !durable {
		_ = os.Remove(backupPath)
		return err
	}

	// Record metadata. This runs for a durable record even when closing the
	// journal reported an error, so rollback can still restore the file.
	m.backups[absPath] = backupEntry{
		tempPath: backupPath,
		mode:     info.Mode(),
	}

	return err
}

// journalPathFor stores paths relative to the transaction root. The journal
// survives a root relocation or a different symlinked spelling of the project,
// while recovery still revalidates every path against the current root.
func (m *Manager) journalPathFor(absPath string) (string, error) {
	rel, err := filepath.Rel(m.rootPath, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to make %s relative to transaction root: %w", absPath, err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to journal path outside transaction root: %s", absPath)
	}
	return filepath.ToSlash(rel), nil
}

// trackCreated journals and records a path the task is about to create.
func (m *Manager) trackCreated(absPath string) error {
	journalPath, err := m.journalPathFor(absPath)
	if err != nil {
		return err
	}
	durable, err := m.appendJournal(journalRecord{Op: journalOpCreate, Path: journalPath})
	if durable {
		// Keep the in-memory entry even when closing the journal failed: the
		// durable record describes this path, so rollback must still remove it.
		m.created = append(m.created, absPath)
		m.createdSet[absPath] = struct{}{}
	}
	return err
}
