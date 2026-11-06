// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewWithGitExcludesTransactionState is a regression test for the git
// template committing scbake's transaction journal. `new --with git` without a
// language pack has no .gitignore, so `git add .` used to stage
// .scbake/tmp/tx-*/journal.jsonl. A clone then failed drift/apply and could
// replay recovery against a checked-out tree. The initial commit must contain
// no .scbake entries, the private state must be gone, and the tree must be
// clean because the finalized manifest is committed too.
func TestNewWithGitExcludesTransactionState(t *testing.T) {
	requireGit(t)
	project := assertNewExcludesTransactionState(t, "new --with git", "new", "demo", "--with", "git")

	if _, statErr := os.Stat(filepath.Join(project, ".scbake")); !os.IsNotExist(statErr) {
		t.Errorf("transaction state was left in the project (stat err: %v)", statErr)
	}
}

// TestNewWithGoAndGitExcludesTransactionState covers the primary
// `new --lang go --with git` flow. The Go pack generates a .gitignore that
// already contains `.scbake/`, which made the literal exclude pathspec fail
// with "paths are ignored by one of your .gitignore files" and deleted the
// project. The initial commit must still succeed, contain no .scbake entries,
// and leave a clean working tree.
func TestNewWithGoAndGitExcludesTransactionState(t *testing.T) {
	requireGit(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}
	assertNewExcludesTransactionState(t, "new --lang go --with git", "new", "demo", "--lang", "go", "--with", "git")
}

// TestNewWithGitHonorsGlobalExcludesFile covers a host configuration whose
// core.excludesFile ignores .scbake. That is a second way .scbake can be
// ignored even without a generated .gitignore, and it must not turn the
// exclude pathspec into a fatal "paths are ignored" error.
func TestNewWithGitHonorsGlobalExcludesFile(t *testing.T) {
	requireGit(t)

	configDir := t.TempDir()
	excludesPath := filepath.Join(configDir, "global-excludes")
	if err := os.WriteFile(excludesPath, []byte(".scbake\n"), 0o600); err != nil {
		t.Fatalf("writing global excludes: %v", err)
	}
	configPath := filepath.Join(configDir, "gitconfig")
	config := "[core]\n\texcludesFile = " + filepath.ToSlash(excludesPath) + "\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("writing global git config: %v", err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "true")
	t.Setenv("GIT_CONFIG_GLOBAL", configPath)

	assertNewExcludesTransactionState(t, "new --with git and a global excludesFile", "new", "demo", "--with", "git")
}

// assertNewExcludesTransactionState runs new in an isolated working directory
// and checks that the initial commit has no .scbake entries and is clean. It
// returns the created project path.
func assertNewExcludesTransactionState(t *testing.T, label string, args ...string) string {
	t.Helper()

	work := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}

	output, err := runCLI(args...)
	if err != nil {
		t.Fatalf("%s failed: %v\nOutput:\n%s", label, err, output)
	}

	project := filepath.Join(work, "demo")
	tracked := gitOutput(t, project, "ls-tree", "-r", "--name-only", "HEAD")
	if strings.Contains(tracked, ".scbake") {
		t.Errorf("initial commit contains scbake transaction state:\n%s", tracked)
	}

	if _, statErr := os.Stat(filepath.Join(project, ".scbake", "tmp")); !os.IsNotExist(statErr) {
		t.Errorf("transaction temp state was left in the project (stat err: %v)", statErr)
	}

	status := strings.TrimSpace(gitOutput(t, project, "status", "--porcelain"))
	if status != "" {
		t.Errorf("working tree is dirty after %s:\n%s", label, status)
	}
	return project
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// gitOutput runs git in dir with global and system configuration isolated so
// the test observes only the repository's own state.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // fixed git command, test-controlled dir
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=true",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed: %v\nOutput:\n%s", args, err, output.String())
	}
	return output.String()
}
