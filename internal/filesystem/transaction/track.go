// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"fmt"
	"os"
	"path/filepath"
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

	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		// Case 1: File/Dir does not exist. We record it as "created".
		m.created = append(m.created, absPath)
		return nil
	}
	if err != nil {
		// Some other error (permission, etc)
		return fmt.Errorf("failed to stat %s: %w", absPath, err)
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

	return m.backupFile(absPath, info)
}

func (m *Manager) resolveAndValidate(path string) (string, error) {
	// Resolve relative paths against the manager's root, not the process
	// working directory, so Track behaves consistently regardless of CWD.
	// Absolute paths are validated against the root via the containment check
	// below, exactly as before.
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(m.rootPath, resolved)
	}

	absPath, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %s: %w", path, err)
	}

	// Security Fix: Ensure path is within root using filepath.Rel
	// This prevents prefix matching bugs (e.g. /tmp/root vs /tmp/root-evil)
	// and handles cross-platform path separators correctly.
	rel, err := filepath.Rel(m.rootPath, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to calculate relative path: %w", err)
	}

	// If the relative path is ".." or starts with "..<separator>", it means
	// absPath is outside m.rootPath. Checking for the separator boundary avoids
	// rejecting legitimate in-root names that merely begin with dots, such as
	// "..hidden.xml".
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"security violation: attempting to track path '%s' outside project root '%s'",
			absPath, m.rootPath,
		)
	}

	return absPath, nil
}

func (m *Manager) alreadyTracked(absPath string) bool {
	if _, backedUp := m.backups[absPath]; backedUp {
		return true
	}
	for _, created := range m.created {
		if created == absPath {
			return true
		}
	}
	return false
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

	// Record metadata
	m.backups[absPath] = backupEntry{
		tempPath: backupPath,
		mode:     info.Mode(),
	}

	return nil
}
