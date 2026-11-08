// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// blockedDirPerms removes write permission so the transaction cleanup cannot
// delete the blocker's child file.
const blockedDirPerms os.FileMode = 0o500

// TestNewKeepsProjectAfterCommittedCleanupError verifies that a cleanup error
// after the commit marker is published becomes a warning, not a command
// failure. The project must remain and the handled-error cleanup must not run.
//
// Version 2 publishes the commit point with an atomic marker instead of a
// committed journal record, so the post-commit fault is injected by making the
// backup-directory cleanup fail rather than by failing a journal Close.
//
//nolint:cyclop // The test builds a fault-injection hook and checks the command outcome.
func TestNewKeepsProjectAfterCommittedCleanupError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based cleanup failure is not portable to Windows")
	}

	resetNewFlags()

	work := t.TempDir()
	t.Chdir(work)

	const project = "committed-cleanup"

	blockerPath := func() string {
		dirs, _ := filepath.Glob(filepath.Join(work, project, fileutil.InternalDir, fileutil.TmpDir, "tx-*"))
		if len(dirs) == 0 {
			return ""
		}
		return filepath.Join(dirs[0], "!cleanup-blocker")
	}

	rootCmd.SetContext(context.Background())
	blockerMade := false
	restoreHook := transaction.SetJournalCloseErrorHookForTest(func(op string) error {
		if blockerMade || op != "create" {
			return nil
		}
		blocker := blockerPath()
		if blocker != "" {
			if err := os.Mkdir(blocker, fileutil.DirPerms); err == nil {
				if err := os.WriteFile(filepath.Join(blocker, "keep"), []byte("keep"), fileutil.PrivateFilePerms); err == nil {
					blockerMade = true
					_ = os.Chmod(blocker, blockedDirPerms)
				}
			}
		}
		return nil
	})
	t.Cleanup(restoreHook)
	t.Cleanup(func() {
		if blocker := blockerPath(); blocker != "" {
			_ = os.Chmod(blocker, fileutil.DirPerms)
		}
	})

	var runErr error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			runErr = executeCLI("new", project, "--with", "makefile")
		})
	})

	if runErr != nil {
		t.Fatalf("new returned %v after a committed cleanup error; output=%s", runErr, stdout)
	}
	if _, err := os.Stat(filepath.Join(work, project, fileutil.ManifestFileName)); err != nil {
		t.Errorf("project was removed after a committed cleanup error: %v", err)
	}
	if strings.Contains(stderr, "Cleaning up failed project directory") {
		t.Errorf("new ran handled-error cleanup after a committed transaction: %s", stderr)
	}
	if !strings.Contains(stderr, "Transaction committed with warning") {
		t.Errorf("new did not report the committed cleanup warning: %q", stderr)
	}
	if !strings.Contains(stderr, "failed to cleanup transaction temp dir") {
		t.Errorf("warning does not preserve the underlying cleanup error: %q", stderr)
	}
	if !strings.Contains(stdout, "Success! New project") {
		t.Errorf("new did not report success after a committed transaction: %q", stdout)
	}
}
