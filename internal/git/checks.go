// Package git provides utilities for running Git commands and performing safety checks.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// runGitCommand is a simple helper for running Git commands.
func runGitCommand(args ...string) (*bytes.Buffer, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)

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
	// Note: runGitCommand now implicitly uses context.Background()
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
		// If the error is due to a command execution failure (ExitError), we assume
		// it is the expected "no commits" state and return nil error.
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			// This is the expected non-zero exit code when HEAD is missing.
			return false, nil // Resolve the original nilerr warning by returning nil error here
		}

		// If it's any other error (e.g., file not found, permission), return the error.
		return false, err
	}
	return true, nil
}
