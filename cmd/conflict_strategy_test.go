// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/templates"
	"strings"
	"testing"
)

// acceptedConflictStrategies is spelled out so the regression test still
// compiles against the pre-fix baseline and fails on behavior, not API.
var acceptedConflictStrategies = []string{
	types.ConflictMerge,
	types.ConflictFail,
	types.ConflictOverwrite,
	types.ConflictArtifact,
	types.ConflictKeepLocal,
}

// TestConflictStrategyInvalidFailsAtCommandEntry is the #261 regression: every
// command that accepts --conflict-strategy must reject a typo before running,
// and its error must name all accepted values.
func TestConflictStrategyInvalidFailsAtCommandEntry(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "apply", args: []string{"apply", "--conflict-strategy", "bogus"}},
		{name: "update", args: []string{"update", "--conflict-strategy", "bogus"}},
		{name: "drift repair", args: []string{"drift", "--repair", "--conflict-strategy", "bogus"}},
		{name: "new", args: []string{"new", "demo", "--conflict-strategy", "bogus"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetFlags()
			tmpDir := t.TempDir()

			oldWD, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd failed: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(oldWD) })
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("Chdir failed: %v", err)
			}

			err = executeCLI(tc.args...)
			if err == nil {
				t.Fatal("invalid --conflict-strategy was accepted")
			}
			if !strings.Contains(err.Error(), "invalid conflict strategy") {
				t.Errorf("error = %q, want it to reject the invalid strategy", err)
			}
			for _, valid := range acceptedConflictStrategies {
				if !strings.Contains(err.Error(), valid) {
					t.Errorf("error = %q, want it to name valid value %q", err, valid)
				}
			}

			if tc.name == "new" {
				if _, statErr := os.Stat(filepath.Join(tmpDir, "demo")); !os.IsNotExist(statErr) {
					t.Errorf("invalid flag created the project directory (stat err: %v)", statErr)
				}
			}
		})
	}
}

// TestApplyInvalidConflictStrategyRunsNoTask proves the rejection happens before
// the plan executes, not merely before it succeeds.
func TestApplyInvalidConflictStrategyRunsNoTask(t *testing.T) {
	resetFlags()
	tmpDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(tmpDir, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms,
	); err != nil {
		t.Fatalf("writing manifest failed: %v", err)
	}

	marker := filepath.Join(tmpDir, "must-not-exist.txt")
	const templateName = "invalid-conflict-strategy-marker"
	templates.Register(templateName, &MockHandlerGeneric{
		Tasks: []types.Task{&MockCreateTask{TargetFile: marker}},
	})

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}

	err = executeCLI("apply", "--with", templateName, "--conflict-strategy", "bogus", ".")
	if err == nil {
		t.Fatal("invalid --conflict-strategy was accepted")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Errorf("a task ran before flag validation (stat err: %v)", statErr)
	}
}

// TestConflictStrategyValidValuesAccepted pins that command entry validation
// does not reject any documented value.
func TestConflictStrategyValidValuesAccepted(t *testing.T) {
	for _, strategy := range acceptedConflictStrategies {
		t.Run(strategy, func(t *testing.T) {
			resetFlags()
			dryRun = true
			tmpDir := t.TempDir()
			if err := os.WriteFile(
				filepath.Join(tmpDir, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms,
			); err != nil {
				t.Fatalf("writing manifest failed: %v", err)
			}

			targetFile := filepath.Join(tmpDir, "file.txt")
			templateName := "valid-conflict-strategy-" + strategy
			templates.Register(templateName, &MockHandlerGeneric{
				Tasks: []types.Task{&MockCreateTask{TargetFile: targetFile}},
			})

			oldWD, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd failed: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(oldWD) })
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("Chdir failed: %v", err)
			}

			captureStdout(t, func() {
				err = executeCLI("apply", "--with", templateName, "--conflict-strategy", strategy, "--dry-run", ".")
			})
			if err != nil {
				t.Errorf("apply --conflict-strategy %s = %v, want nil", strategy, err)
			}
		})
	}
}
