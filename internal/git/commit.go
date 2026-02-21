// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package git

import (
	"fmt"
)

// CommitChanges stages all changes and commits them with a given message.
func CommitChanges(message string) error {
	// 1. Stage all changes
	if _, err := runGitCommand("add", "."); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}

	// 2. Check if there are any staged changes
	// 'git diff --cached --quiet' exits 0 if no changes, 1 if changes
	_, err := runGitCommand("diff", "--cached", "--quiet")
	if err == nil {
		// err is nil, which means 'git diff' exited 0: no changes.
		// This is not an error, it just means that there is nothing to commit.
		return nil
	}

	// If err is not nil, 'git diff' exited 1, meaning there are
	// staged changes, so we proceed to commit.

	// 3. Commit
	if _, err := runGitCommand("commit", "-m", message); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	return nil
}

// InitialCommit creates an empty initial commit.
// This is necessary to make HEAD a valid ref for subsequent operations.
func InitialCommit(message string) error {
	_, err := runGitCommand("commit", "--allow-empty", "-m", message)
	if err != nil {
		return fmt.Errorf("git initial commit failed: %w", err)
	}
	return nil
}
