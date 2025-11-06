package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

// runGitCommand is a simple helper for running Git commands.
func runGitCommand(args ...string) (*bytes.Buffer, error) {
	cmd := exec.Command("git", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Use stderr for the error message if available
		if stderr.Len() > 0 {
			return nil, errors.New(stderr.String())
		}
		return nil, err
	}
	return &stdout, nil
}

// CheckGitInstalled verifies 'git' is in the user's PATH.
func CheckGitInstalled() error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("'git' command not found in $PATH. Please install Git")
	}
	return nil
}

// CheckIsRepo verifies the current directory is a Git repository.
func CheckIsRepo() error {
	_, err := runGitCommand("rev-parse", "--is-inside-work-tree")
	if err != nil {
		return errors.New("not a git repository (or any of the parent directories). Please run 'git init'")
	}
	return nil
}

// CheckIsClean verifies the Git working tree is clean (no uncommitted changes).
func CheckIsClean() error {
	out, err := runGitCommand("status", "--porcelain")
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	if out.String() != "" {
		return errors.New("uncommitted changes in Git working tree. Please commit or stash your changes before running 'scbake apply'")
	}
	return nil
}

// CheckHasHEAD checks if HEAD is a valid ref (i.e., if there is at least one commit).
func CheckHasHEAD() (bool, error) {
	_, err := runGitCommand("rev-parse", "HEAD")
	if err != nil {
		// "fatal: Failed to resolve 'HEAD' as a valid ref" will trigger this
		return false, nil
	}
	return true, nil
}
