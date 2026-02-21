// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package git

import (
	"fmt"
	"time"
)

// CreateSavepoint creates a simple Git tag to act as a rollback point.
// It returns the unique tag name.
func CreateSavepoint() (string, error) {
	// Generate a unique tag name
	tagName := fmt.Sprintf("scbake-savepoint-%d", time.Now().UnixNano())

	_, err := runGitCommand("tag", tagName)
	if err != nil {
		return "", fmt.Errorf("failed to create git savepoint: %w", err)
	}
	return tagName, nil
}

// RollbackToSavepoint reverts the repo to the savepoint.
func RollbackToSavepoint(tagName string) error {
	// 1. Reset all tracked files to the specific savepoint tag.
	if _, err := runGitCommand("reset", "--hard", tagName); err != nil {
		return fmt.Errorf("git reset to savepoint failed: %w", err)
	}

	// 2. Remove all untracked files and directories that may have been created.
	if _, err := runGitCommand("clean", "-fd"); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	// 3. Delete the savepoint tag after completing rollback.
	if err := DeleteSavepoint(tagName); err != nil {
		return fmt.Errorf("failed to delete savepoint tag during rollback: %w", err)
	}
	return nil
}

// DeleteSavepoint removes the tag after a successful operation.
func DeleteSavepoint(tagName string) error {
	if _, err := runGitCommand("tag", "-d", tagName); err != nil {
		return fmt.Errorf("failed to delete git savepoint tag: %w", err)
	}
	return nil
}
