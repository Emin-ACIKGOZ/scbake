// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// TestNewKeepsProjectAfterDurableCommitCloseError verifies that a Close error
// after the committed journal record is durable becomes a warning, not a
// command failure. The project must remain and the handled-error cleanup must
// not run.
func TestNewKeepsProjectAfterDurableCommitCloseError(t *testing.T) {
	resetNewFlags()

	work := t.TempDir()
	t.Chdir(work)

	rootCmd.SetContext(context.Background())
	restoreHook := transaction.SetJournalCloseErrorHookForTest(func(op string) error {
		if op == "committed" {
			return errors.New("injected committed record close failure")
		}
		return nil
	})
	t.Cleanup(restoreHook)

	const project = "durable-close"
	var runErr error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			runErr = executeCLI("new", project, "--with", "makefile")
		})
	})

	if runErr != nil {
		t.Fatalf("new returned %v after a durable commit close error; output=%s", runErr, stdout)
	}
	if _, err := os.Stat(filepath.Join(work, project, fileutil.ManifestFileName)); err != nil {
		t.Errorf("project was removed after a durable commit close error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, project, fileutil.InternalDir, fileutil.TmpDir)); !os.IsNotExist(err) {
		t.Errorf("durable commit left transaction temp state behind (stat err: %v)", err)
	}
	if strings.Contains(stderr, "Cleaning up failed project directory") {
		t.Errorf("new ran handled-error cleanup after a durable commit: %s", stderr)
	}
	if !strings.Contains(stderr, "injected committed record close failure") {
		t.Errorf("warning does not preserve the underlying close error: %q", stderr)
	}
	if !strings.Contains(stdout, "Success! New project") {
		t.Errorf("new did not report success after a durable commit: %q", stdout)
	}
}
