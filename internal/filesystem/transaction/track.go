// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
)

// Track registers a path that is about to be modified or created.
// It creates a backup if the file exists, or records it for deletion if it doesn't.
//
// Track does not record content identity: a created path tracked this way is
// kept on rollback unless it is an unchanged empty directory. Use TrackWrite
// when the exact bytes that will be written are already known.
func (m *Manager) Track(path string) error {
	return m.track(path, "")
}

// TrackWrite registers a path that is about to be created or overwritten with
// content. For a path the transaction is creating, it journals the raw,
// byte-exact SHA-256 of content before the caller writes it, so rollback only
// removes bytes that still match what this transaction intended to write.
//
// An existing path is still backed up as usual; its content hash is not used
// because restoring the backup is authoritative.
func (m *Manager) TrackWrite(path string, content []byte) error {
	return m.track(path, hashBytes(content))
}

// SnapshotCreatedContent records the current raw SHA-256 of a created regular
// file when no hash has been recorded yet. It is for opaque writers (commands,
// archive extraction, the manifest save) whose bytes are only observable after
// the write. It is a no-op for paths this transaction did not create, absent
// paths, and non-regular paths. The record is journaled and the hash is only
// remembered once it is durable.
func (m *Manager) SnapshotCreatedContent(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	absPath, err := m.resolveAndValidate(path)
	if err != nil {
		return err
	}
	if _, ok := m.createdSet[absPath]; !ok {
		return nil
	}
	if m.createdHashes[absPath] != "" {
		return nil
	}

	info, err := os.Lstat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to snapshot created path %s: %w", absPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}

	hash, err := hashFile(m.rootPath, absPath)
	if err != nil {
		return fmt.Errorf("failed to hash created path %s: %w", absPath, err)
	}
	return m.trackCreated(absPath, hash)
}

// hashBytes returns the raw, byte-exact SHA-256 of content as lower-case hex.
// It deliberately does not normalize line endings: rollback authority is over
// the exact bytes the transaction wrote, not a text projection of them.
func hashBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// hashFile returns the raw SHA-256 of the file at absPath, reading it through
// the symlink-safe, no-follow helper.
func hashFile(rootPath, absPath string) (string, error) {
	f, err := fileutil.SafeOpenFile(rootPath, absPath, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// track is the shared implementation of Track and TrackWrite. A non-empty hash
// only applies to a path the transaction creates; an existing path is backed up
// and restored instead.
func (m *Manager) track(path, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	absPath, err := m.resolveAndValidate(path)
	if err != nil {
		return err
	}

	// Check if we have already tracked this file to avoid double-backup overhead.
	if m.alreadyTracked(absPath) {
		if hash == "" {
			return nil
		}
		// A path already recorded as created (for example Track followed by
		// TrackWrite in the append task) can still gain a content hash. A
		// backed-up path is restored from its backup, so a hash adds nothing.
		if _, created := m.createdSet[absPath]; created {
			return m.trackCreated(absPath, hash)
		}
		return nil
	}

	info, err := os.Lstat(absPath)
	if os.IsNotExist(err) {
		// Case 1: File/Dir does not exist. Journal it as created before the
		// task writes, so a crash after the write can still remove it.
		return m.trackCreated(absPath, hash)
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

// trackCreated journals and records a path the task is about to create. A
// non-empty hash is journaled before the write so rollback can verify that the
// bytes still belong to this transaction. A duplicate record for an already
// created path updates only the recorded hash.
func (m *Manager) trackCreated(absPath, hash string) error {
	if hash != "" && m.createdHashes[absPath] == hash {
		return nil
	}
	journalPath, err := m.journalPathFor(absPath)
	if err != nil {
		return err
	}
	durable, err := m.appendJournal(journalRecord{
		Op:   journalOpCreate,
		Path: journalPath,
		Hash: hash,
	})
	if durable {
		// Keep the in-memory entry even when closing the journal failed: the
		// durable record describes this path, so rollback must still remove it.
		if _, exists := m.createdSet[absPath]; !exists {
			m.created = append(m.created, absPath)
			m.createdSet[absPath] = struct{}{}
		}
		if hash != "" {
			m.createdHashes[absPath] = hash
		}
	}
	return err
}
