// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package transaction provides a filesystem-based undo log for atomic file operations.
package transaction

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const tempDirPerm os.FileMode = 0o750

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
}

// New creates a new transaction manager scoped to the given project root.
func New(rootPath string) (*Manager, error) {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute root path: %w", err)
	}

	return &Manager{
		rootPath: absRoot,
		backups:  make(map[string]backupEntry),
		created:  make([]string, 0),
	}, nil
}

// Commit finalizes the transaction by deleting the temporary backup directory and pruning structural scaffolding.
// This should be called only after all tasks have succeeded.
func (m *Manager) Commit() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If tempDir was never created, there is nothing to clean up.
	if m.tempDir == "" {
		return nil
	}

	// Remove the temp directory and all backed-up files.
	if err := os.RemoveAll(m.tempDir); err != nil {
		return fmt.Errorf("failed to cleanup transaction temp dir: %w", err)
	}

	// Prune parent scaffolding if empty (.scbake/tmp and .scbake)
	m.cleanupStructure()

	// Reset internal state
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
	dirName := fmt.Sprintf("tx-%d", timestamp)

	// We place tmp inside the project root to ensure we are on the same filesystem/partition,
	// which makes file moves atomic and avoids cross-device link errors.
	path := filepath.Join(m.rootPath, ".scbake", "tmp", dirName)

	if err := os.MkdirAll(path, tempDirPerm); err != nil {
		return fmt.Errorf("failed to create temp dir %s: %w", path, err)
	}

	m.tempDir = path
	return nil
}

// cleanupStructure attempts to prune .scbake/tmp and .scbake.
// It uses os.Remove which only succeeds if the directory is empty.
func (m *Manager) cleanupStructure() {
	tmpParent := filepath.Join(m.rootPath, ".scbake", "tmp")
	scbakeRoot := filepath.Join(m.rootPath, ".scbake")

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
