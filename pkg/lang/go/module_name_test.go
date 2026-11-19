// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModuleNameForRejectsReservedTargets is the command-adjacent guard for
// #263: a filesystem-root or parent target must fail at plan time instead of
// producing "/" or ".." as a module path.
func TestModuleNameForRejectsReservedTargets(t *testing.T) {
	for _, target := range []string{"/", "..", ".", ""} {
		if target == "." {
			continue // "." is a legitimate real-`new` target, covered below.
		}
		if name, err := moduleNameFor(target, nil); err == nil {
			t.Errorf("moduleNameFor(%q) = %q, want an error", target, name)
		}
	}
}

// TestModuleNameForDotUsesInvocationDirectory keeps the real-`new` path working:
// "." resolves to the directory the process is already inside.
func TestModuleNameForDotUsesInvocationDirectory(t *testing.T) {
	dir := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	want := strings.ToLower(filepath.Base(dir))
	for _, target := range []string{".", "./", ".//", "./."} {
		name, err := moduleNameFor(target, nil)
		if err != nil {
			t.Fatalf("moduleNameFor(%q) failed: %v", target, err)
		}
		if name != want {
			t.Errorf("moduleNameFor(%q) = %q, want %q", target, name, want)
		}
	}
}
