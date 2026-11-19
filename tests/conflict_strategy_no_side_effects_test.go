// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInvalidConflictStrategyRunsNoCommand is the #261 follow-up: command-entry
// validation must reject an unknown strategy before new/update/drift mutate
// anything, not merely produce a different error message.
func TestInvalidConflictStrategyRunsNoCommand(t *testing.T) { //nolint:cyclop // three independent command paths
	t.Run("new creates nothing", func(t *testing.T) {
		work := t.TempDir()
		t.Chdir(work)

		out, err := runCLI("new", "proj", "--with", "makefile", "--conflict-strategy", "bogus")
		if err == nil {
			t.Fatalf("new accepted an invalid strategy:\n%s", out)
		}
		if _, statErr := os.Stat(filepath.Join(work, "proj")); !os.IsNotExist(statErr) {
			t.Errorf("rejected new left a directory behind (stat err: %v)", statErr)
		}
	})

	t.Run("update rewrites nothing", func(t *testing.T) {
		work := t.TempDir()
		t.Chdir(work)
		if out, err := runCLI("new", "proj", "--with", "makefile"); err != nil {
			t.Fatalf("prepare project: %v\n%s", err, out)
		}
		project := filepath.Join(work, "proj")
		t.Chdir(project)
		makefile := filepath.Join(project, "Makefile")
		if err := os.Remove(makefile); err != nil {
			t.Fatalf("remove Makefile: %v", err)
		}

		out, err := runCLI("update", "--conflict-strategy", "bogus")
		if err == nil {
			t.Fatalf("update accepted an invalid strategy:\n%s", out)
		}
		if _, statErr := os.Stat(makefile); !os.IsNotExist(statErr) {
			t.Errorf("rejected update recreated Makefile (stat err: %v)", statErr)
		}
	})

	t.Run("drift repair rewrites nothing", func(t *testing.T) {
		work := t.TempDir()
		t.Chdir(work)
		if out, err := runCLI("new", "proj", "--with", "makefile"); err != nil {
			t.Fatalf("prepare project: %v\n%s", err, out)
		}
		project := filepath.Join(work, "proj")
		t.Chdir(project)
		makefile := filepath.Join(project, "Makefile")
		if err := os.Remove(makefile); err != nil {
			t.Fatalf("remove Makefile: %v", err)
		}

		out, err := runCLI("drift", "--repair", "--conflict-strategy", "bogus")
		if err == nil {
			t.Fatalf("drift --repair accepted an invalid strategy:\n%s", out)
		}
		if _, statErr := os.Stat(makefile); !os.IsNotExist(statErr) {
			t.Errorf("rejected drift --repair recreated Makefile (stat err: %v)", statErr)
		}
	})
}
