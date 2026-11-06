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
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	work := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}

	output, err := runCLI("new", "demo", "--with", "git")
	if err != nil {
		t.Fatalf("new --with git failed: %v\nOutput:\n%s", err, output)
	}

	project := filepath.Join(work, "demo")
	tracked := gitOutput(t, project, "ls-tree", "-r", "--name-only", "HEAD")
	if strings.Contains(tracked, ".scbake") {
		t.Errorf("initial commit contains scbake transaction state:\n%s", tracked)
	}

	if _, statErr := os.Stat(filepath.Join(project, ".scbake")); !os.IsNotExist(statErr) {
		t.Errorf("transaction state was left in the project (stat err: %v)", statErr)
	}

	status := strings.TrimSpace(gitOutput(t, project, "status", "--porcelain"))
	if status != "" {
		t.Errorf("working tree is dirty after `new --with git`:\n%s", status)
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
