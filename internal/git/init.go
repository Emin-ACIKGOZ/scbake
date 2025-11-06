package git

import "fmt"

// Init runs `git init` in the current directory.
func Init() error {
	if _, err := runGitCommand("init"); err != nil {
		return fmt.Errorf("git init failed: %w", err)
	}
	// The default branch can be set here, if desired.
	if _, err := runGitCommand("branch", "-M", "main"); err != nil {
		return fmt.Errorf("failed to set default branch to 'main': %w", err)
	}
	return nil
}
