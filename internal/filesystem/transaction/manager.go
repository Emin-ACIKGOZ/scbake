// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
	"sync"
	"time"
)

// staleTransactionThreshold is the minimum age a transaction backup directory
// must reach before a startup sweep reclaims it. A live apply touches its
// transaction directory on every backup and the apply timeout is 30 minutes,
// so a directory idle for a full hour can only be the residue of a crash
// (panic/SIGKILL/power loss) that skipped both Commit and Rollback.
const staleTransactionThreshold = time.Hour

// txDirPrefix identifies transaction backup directories created by ensureTempDir.
const txDirPrefix = "tx-"

// backupEntry holds the metadata required to restore a file.
type backupEntry struct {
	tempPath string
	mode     os.FileMode
}

// Manager handles atomic filesystem operations by tracking changes and providing rollback capabilities.
type Manager struct {
	mu sync.Mutex

	// rootPath is the absolute path to the project root.
	// Safety check: The manager will refuse to touch files outside this root.
	rootPath string

	// tempDir is the hidden directory where backups for this specific transaction are stored.
	// It is lazily created only when the first backup is needed.
	tempDir string

	// backups maps the original absolute path to its backup metadata.
	backups map[string]backupEntry

	// created tracks absolute paths of files/dirs created during the transaction.
	// They are stored in order of creation (append-only) for LIFO deletion.
	created []string

	// committed records that the transaction was finalized with Commit().
	// After a commit, Rollback() must be a no-op: the deferred rollback safety
	// net (RunApply) runs after Commit() even on success.
	committed bool
}

// New creates a new transaction manager scoped to the given project root.
func New(rootPath string) (*Manager, error) {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute root path: %w", err)
	}

	// Best-effort reclamation of transaction dirs orphaned by a crashed apply.
	// A failure here must never prevent a new transaction from starting.
	sweepStaleTransactions(absRoot)

	return &Manager{
		rootPath: absRoot,
		backups:  make(map[string]backupEntry),
		created:  make([]string, 0),
	}, nil
}

// sweepStaleTransactions removes transaction backup directories under
// <root>/.scbake/tmp that are older than staleTransactionThreshold. Such
// directories are left behind when a crash skips both Commit and Rollback.
// It is a best-effort, no-op operation: a missing directory or any filesystem
// error is ignored.
func sweepStaleTransactions(rootPath string) {
	tmpDir := filepath.Join(rootPath, fileutil.InternalDir, fileutil.TmpDir)

	// Resolve symlinks and refuse to sweep a temp directory that escapes the
	// project root (for example when .scbake/tmp is a symlink to shared
	// storage). The manager must never delete paths outside its root.
	resolvedRoot, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return
	}
	resolvedTmp, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		return // No temp directory yet.
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedTmp)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}

	entries, err := os.ReadDir(resolvedTmp)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-staleTransactionThreshold)
	for _, entry := range entries {
		// entry.IsDir() is false for symlink entries, so a tx-* symlink is
		// skipped rather than followed.
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), txDirPrefix) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}

		_ = os.RemoveAll(filepath.Join(resolvedTmp, entry.Name()))
	}
}

// Root returns the absolute project root the transaction is scoped to. Callers
// that create paths inside the root use it to bound ancestor tracking.
func (m *Manager) Root() string {
	return m.rootPath
}

// Commit finalizes the transaction by deleting the temporary backup directory and pruning structural scaffolding.
// This should be called only after all tasks have succeeded.
// After Commit, Rollback is a no-op: the transaction's changes are permanent.
func (m *Manager) Commit() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.committed {
		return nil
	}
	m.committed = true

	// If tempDir was never created, there is nothing to clean up.
	if m.tempDir != "" {
		// Remove the temp directory and all backed-up files.
		if err := os.RemoveAll(m.tempDir); err != nil {
			return fmt.Errorf("failed to cleanup transaction temp dir: %w", err)
		}

		// Prune parent scaffolding if empty (.scbake/tmp and .scbake)
		m.cleanupStructure()
	}

	// Reset internal state so a trailing Rollback cannot undo committed changes.
	m.resetState()

	return nil
}

// ensureTempDir creates the hidden temporary directory if it doesn't exist.
// This is called lazily by Track().
func (m *Manager) ensureTempDir() error {
	if m.tempDir != "" {
		return nil
	}

	// Create a unique temp dir inside .scbake/tmp
	timestamp := time.Now().UnixNano()
	dirName := fmt.Sprintf("%s%d", txDirPrefix, timestamp)

	// We place tmp inside the project root to ensure we are on the same filesystem/partition,
	// which makes file moves atomic and avoids cross-device link errors.
	path := filepath.Join(m.rootPath, fileutil.InternalDir, fileutil.TmpDir, dirName)

	if err := os.MkdirAll(path, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create temp dir %s: %w", path, err)
	}

	m.tempDir = path
	return nil
}

// cleanupStructure attempts to prune .scbake/tmp and .scbake.
// It uses os.Remove which only succeeds if the directory is empty.
func (m *Manager) cleanupStructure() {
	tmpParent := filepath.Join(m.rootPath, fileutil.InternalDir, fileutil.TmpDir)
	scbakeRoot := filepath.Join(m.rootPath, fileutil.InternalDir)

	// Best effort removal of parent directories
	_ = os.Remove(tmpParent)
	_ = os.Remove(scbakeRoot)
}

// resetState clears the internal trackers for a fresh state.
func (m *Manager) resetState() {
	m.tempDir = ""
	m.backups = make(map[string]backupEntry)
	m.created = make([]string, 0)
}
