package git

import (
	"fmt"
)

// CommitChanges stages all changes and commits them with a given message.
func CommitChanges(message string) error {
	// Stage all changes
	if _, err := runGitCommand("add", "."); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}

	// Commit
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
