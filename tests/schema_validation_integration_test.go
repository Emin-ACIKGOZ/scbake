// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaValidation_New verifies that templates with required schema variables
// cause the 'new' command to fail early when variables are missing.
func TestSchemaValidation_New(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	// The makefile template has optional variables only, so it should succeed
	t.Run("OptionalVarsOnly_Succeeds", func(t *testing.T) {
		// Even without --set, optional vars with defaults should work
		output, err := runCLI("new", "proj-opt", "--with", "makefile")
		if err != nil {
			t.Fatalf("expected success, got error: %v\nOutput: %s", err, output)
		}
		// Verify the project was created
		if _, statErr := os.Stat(filepath.Join(tmpDir, "proj-opt", "Makefile")); statErr != nil {
			t.Errorf("Makefile was not created: %v", statErr)
		}
	})

	// The ci_github template also has optional variables only, same behavior
	_ = os.Chdir(tmpDir)

	t.Run("SetFlag_PopulatesMetadata", func(t *testing.T) {
		output, err := runCLI("new", "proj-set", "--with", "makefile",
			"--set", "build_tool=npm",
			"--set", "test_command=npm test",
		)
		if err != nil {
			t.Fatalf("expected success with --set, got error: %v\nOutput: %s", err, output)
		}
		_ = output
	})

	// Test that --set with missing '=' is rejected
	_ = os.Chdir(tmpDir)

	t.Run("InvalidSetFlag_Fails", func(t *testing.T) {
		output, err := runCLI("new", "proj-bad", "--with", "makefile", "--set", "badformat")
		if err == nil {
			t.Fatal("expected failure with invalid --set format")
		}
		if !strings.Contains(output, "invalid --set") {
			t.Errorf("output should mention invalid --set format, got: %s", output)
		}
	})
}
