// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// TestRecoverCommandHonorsCanceledContext verifies that `recover` returns the
// context error instead of reporting that no transaction was found after a
// SIGINT/SIGTERM canceled the command.
func TestRecoverCommandHonorsCanceledContext(t *testing.T) {
	resetFlags()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rootCmd.SetContext(ctx)
	t.Cleanup(func() {
		rootCmd.SetContext(context.Background())
		recoverCmd.SetContext(context.Background())
	})

	var err error
	output := captureStdout(t, func() {
		err = executeCLI("recover", t.TempDir())
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("recover error = %v, want context.Canceled", err)
	}
	if strings.Contains(output, "No interrupted transaction found") {
		t.Errorf("recover reported no transaction after cancellation: %q", output)
	}
}

// TestRecoverDryRunNamesCanonicalRootForSymlink verifies that dry-run
// disclosure names the real project directory when recover is invoked through
// an alias symlink.
func TestRecoverDryRunNamesCanonicalRootForSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	resetFlags()
	rootCmd.SetContext(context.Background())
	recoverCmd.SetContext(context.Background())

	work := t.TempDir()
	realProject := filepath.Join(work, "real")
	txDir := filepath.Join(realProject, fileutil.InternalDir, fileutil.TmpDir, "tx-new")
	if err := os.MkdirAll(txDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	header := `{"version":1,"pid":1073741824,"remove_root":true}` + "\n"
	if err := os.WriteFile(filepath.Join(txDir, "journal.jsonl"), []byte(header), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	alias := filepath.Join(work, "alias")
	if err := os.Symlink(realProject, alias); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)

	var err error
	output := captureStdout(t, func() {
		err = executeCLI("recover", "--dry-run", "alias")
	})
	if err != nil {
		t.Fatalf("recover --dry-run failed: %v\nOutput: %s", err, output)
	}
	if !strings.Contains(output, txDir) {
		t.Errorf("recover --dry-run did not name the canonical transaction directory: %q", output)
	}
	if strings.Contains(output, alias) {
		t.Errorf("recover --dry-run named the alias instead of the real root: %q", output)
	}
}
