// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveTemplateDir pins the helper that normalizes and validates the
// --template-dir/SCBAKE_TEMPLATE_DIR override before a command uses it.
func TestResolveTemplateDir(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)

	existing := filepath.Join(work, "overrides")
	if err := os.MkdirAll(existing, 0o755); err != nil { //nolint:gosec // test fixture directory
		t.Fatalf("MkdirAll(%q) failed: %v", existing, err)
	}
	regular := filepath.Join(work, "not-a-dir")
	if err := os.WriteFile(regular, []byte("not a directory"), 0o644); err != nil { //nolint:gosec // test fixture file
		t.Fatalf("WriteFile(%q) failed: %v", regular, err)
	}

	relativeAbs, err := filepath.Abs("overrides")
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty", raw: "", want: ""},
		{name: "absolute existing directory", raw: existing, want: existing},
		{name: "relative directory", raw: "overrides", want: relativeAbs},
		{name: "regular file", raw: regular, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveTemplateDir(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveTemplateDir(%q) = %q, nil; want an error", tc.raw, got)
				}
				if !strings.Contains(err.Error(), filepath.Base(regular)) {
					t.Errorf("resolveTemplateDir(%q) error = %v, want it to name the rejected path", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveTemplateDir(%q) = %v, want nil", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("resolveTemplateDir(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestRunNew_RelativeTemplateDirOverride is a regression test for #141: a
// relative --template-dir must be resolved against the invocation working
// directory, not the newly created project directory that 'new' chdirs into.
func TestRunNew_RelativeTemplateDirOverride(t *testing.T) {
	resetNewFlags()

	work := t.TempDir()
	t.Chdir(work)

	overrideDir := filepath.Join(work, "ovr")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil { //nolint:gosec // test fixture directory
		t.Fatalf("MkdirAll(%q) failed: %v", overrideDir, err)
	}
	const overrideContent = "root = true\n# RELATIVE OVERRIDE\n"
	if err := os.WriteFile(filepath.Join(overrideDir, ".editorconfig.tpl"), []byte(overrideContent), 0o644); err != nil { //nolint:gosec // test fixture file
		t.Fatalf("WriteFile override failed: %v", err)
	}

	templateDirFlag = "ovr"
	newWithFlag = []string{"editorconfig"}

	const project = "relative-override"
	created := false
	if err := runNew(context.Background(), project, &created); err != nil {
		t.Fatalf("runNew(%q) = %v, want nil", project, err)
	}

	content, err := os.ReadFile(filepath.Join(work, project, ".editorconfig")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("reading generated .editorconfig failed: %v", err)
	}
	if !strings.Contains(string(content), "RELATIVE OVERRIDE") {
		t.Errorf(".editorconfig did not use the relative override directory, got:\n%s", string(content))
	}
}

// TestRunNew_MissingTemplateDirFailsBeforeCreatingProject is a regression test
// for #141: a missing override directory must fail before the project directory
// is created instead of silently falling back to the built-in templates.
func TestRunNew_MissingTemplateDirFailsBeforeCreatingProject(t *testing.T) {
	resetNewFlags()

	work := t.TempDir()
	t.Chdir(work)

	missing := filepath.Join(work, "does-not-exist")
	templateDirFlag = missing
	newWithFlag = []string{"editorconfig"}

	const project = "missing-override"
	created := false
	err := runNew(context.Background(), project, &created)
	if err == nil {
		t.Fatalf("runNew(%q) = nil, want an error for a missing template directory", project)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("runNew(%q) error = %v, want it to name the resolved path %q", project, err, missing)
	}
	if created {
		t.Errorf("runNew(%q) created the project directory despite the missing template directory", project)
	}
	if _, statErr := os.Stat(filepath.Join(work, project)); !os.IsNotExist(statErr) {
		t.Errorf("runNew(%q) left a project directory behind (stat err: %v)", project, statErr)
	}
}

// TestApply_MissingTemplateDirFailsWithoutWriting is a regression test for #141:
// 'apply' must reject a missing override directory before any file is written.
func TestApply_MissingTemplateDirFailsWithoutWriting(t *testing.T) {
	resetFlags()

	work := t.TempDir()
	t.Chdir(work)
	if err := os.WriteFile(filepath.Join(work, ".scbake.toml"), nil, 0o600); err != nil {
		t.Fatalf("writing empty manifest failed: %v", err)
	}

	missing := filepath.Join(work, "does-not-exist")
	templateDirFlag = missing

	err := executeCLI("apply", "--with", "editorconfig", ".")
	if err == nil {
		t.Fatal("apply with a missing template directory = nil, want an error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("apply error = %v, want it to name the resolved path %q", err, missing)
	}
	if _, statErr := os.Stat(filepath.Join(work, ".editorconfig")); !os.IsNotExist(statErr) {
		t.Errorf("apply wrote .editorconfig despite the missing template directory (stat err: %v)", statErr)
	}
}
