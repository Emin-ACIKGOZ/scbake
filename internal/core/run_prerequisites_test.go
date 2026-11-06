// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeBinaryFile returns the on-disk name for a fake executable. Windows only
// resolves executables through PATHEXT, so use a .cmd shim there.
func fakeBinaryFile(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".cmd"
	}
	return name
}

// installFakeBinary writes a no-op executable named after bin into dir.
func installFakeBinary(t *testing.T, dir, bin string) {
	t.Helper()

	path := filepath.Join(dir, fakeBinaryFile(bin))
	content := []byte("#!/bin/sh\nexit 0\n")
	if runtime.GOOS == "windows" {
		content = []byte("@echo off\r\nexit /b 0\r\n")
	}
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write fake %s binary: %v", bin, err)
	}
}

// setControlledPath replaces PATH with a temporary directory containing only
// the requested fake binaries and returns that directory.
func setControlledPath(t *testing.T, binaries ...string) string {
	t.Helper()

	dir := t.TempDir()
	for _, bin := range binaries {
		installFakeBinary(t, dir, bin)
	}

	t.Setenv("PATH", dir)
	if runtime.GOOS == "windows" {
		// t.Setenv restores the original PATHEXT; set it explicitly so .cmd
		// shims resolve even in environments with an empty PATHEXT.
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}

	return dir
}

// TestCheckLangPrerequisitesSpringWithoutJava is the regression test for
// issue #163: spring scaffolding runs curl, unzip, rm and chmod only, so java
// must not be required to create a project.
func TestCheckLangPrerequisitesSpringWithoutJava(t *testing.T) {
	setControlledPath(t, "curl", "unzip")

	// Guard that the controlled PATH really does not expose java; otherwise
	// the assertion below could pass for the wrong reason.
	if javaPath, err := exec.LookPath("java"); err == nil {
		t.Fatalf("test setup leaked system java at %q", javaPath)
	}

	if err := CheckLangPrerequisites("spring"); err != nil {
		t.Fatalf("CheckLangPrerequisites(spring) with curl+unzip but no java = %v, want nil", err)
	}
}

// TestCheckLangPrerequisitesSpringMissingBinaries verifies that spring
// scaffolding still requires the binaries it actually uses.
func TestCheckLangPrerequisitesSpringMissingBinaries(t *testing.T) {
	tests := []struct {
		name      string
		available []string
		missing   string
	}{
		{name: "missing curl", available: []string{"unzip"}, missing: "curl"},
		{name: "missing unzip", available: []string{"curl"}, missing: "unzip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setControlledPath(t, tt.available...)

			err := CheckLangPrerequisites("spring")
			if err == nil {
				t.Fatalf("CheckLangPrerequisites(spring) without %s = nil, want error", tt.missing)
			}
			if !strings.Contains(err.Error(), tt.missing) {
				t.Fatalf("CheckLangPrerequisites(spring) error = %q, want it to mention %q", err, tt.missing)
			}
		})
	}
}

// TestCheckLangPrerequisitesOtherLanguages locks in the preflight requirements
// for the non-spring languages.
func TestCheckLangPrerequisitesOtherLanguages(t *testing.T) {
	tests := []struct {
		lang   string
		binary string
	}{
		{lang: "go", binary: "go"},
		{lang: "svelte", binary: "npm"},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			t.Run("binary present", func(t *testing.T) {
				setControlledPath(t, tt.binary)

				if err := CheckLangPrerequisites(tt.lang); err != nil {
					t.Fatalf("CheckLangPrerequisites(%s) with %s = %v, want nil", tt.lang, tt.binary, err)
				}
			})

			t.Run("binary absent", func(t *testing.T) {
				setControlledPath(t)

				err := CheckLangPrerequisites(tt.lang)
				if err == nil {
					t.Fatalf("CheckLangPrerequisites(%s) without %s = nil, want error", tt.lang, tt.binary)
				}
				if !strings.Contains(err.Error(), tt.binary) {
					t.Fatalf("CheckLangPrerequisites(%s) error = %q, want it to mention %q", tt.lang, err, tt.binary)
				}
			})
		})
	}
}
