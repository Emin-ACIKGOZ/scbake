// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// appendLine adds a local edit to a managed file.
func appendLine(t *testing.T, path, line string) {
	t.Helper()
	//nolint:gosec // test-controlled temporary path
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		t.Fatalf("append %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

// TestResidualDriftExitContract pins the documented exit contract: update and
// drift --repair exit 0 when they complete with residual drift, while drift is
// the authoritative gate and accept clears an intentional edit.
func TestResidualDriftExitContract(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)

	if out, err := runCLI("new", "demo", "--lang", "go"); err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	project := filepath.Join(work, "demo")
	t.Chdir(project)
	mainGo := filepath.Join(project, "main.go")

	appendLine(t, mainGo, "// intentional local edit\n")

	// update completes with merge, which preserves the local edit.
	out, err := runCLI("update")
	if err != nil {
		t.Fatalf("update should complete with residual drift, got error %v\n%s", err, out)
	}

	// drift is the gate and must fail while the edit remains.
	out, err = runCLI("drift")
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("drift should exit non-zero for residual drift, got err=%v\n%s", err, out)
	}
	if code := exitErr.ExitCode(); code != 1 {
		t.Fatalf("drift exit code = %d, want 1\n%s", code, out)
	}

	// accept records the intentional edit, making the gate clean.
	if out, err := runCLI("accept", "main.go"); err != nil {
		t.Fatalf("accept failed: %v\n%s", err, out)
	}
	if out, err := runCLI("drift"); err != nil {
		t.Fatalf("drift should be clean after accept, got %v\n%s", err, out)
	}

	// A second edit exercises the repair completion contract.
	appendLine(t, mainGo, "// second local edit\n")
	out, err = runCLI("drift", "--repair")
	if err != nil {
		t.Fatalf("drift --repair should complete with residual drift, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "still drifted") {
		t.Errorf("drift --repair should report residual drift, output:\n%s", out)
	}
}
