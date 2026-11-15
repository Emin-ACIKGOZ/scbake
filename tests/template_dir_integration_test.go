// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTemplateDir_EnvRelativeResolvedAtInvocationCwd is a regression test for
// #141: a relative SCBAKE_TEMPLATE_DIR must resolve against the directory the
// command was invoked from, before 'new' chdirs into the new project.
func TestTemplateDir_EnvRelativeResolvedAtInvocationCwd(t *testing.T) {
	work := t.TempDir()

	overrideDir := filepath.Join(work, "ovr")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil { //nolint:gosec // test fixture directory
		t.Fatalf("MkdirAll(%q) failed: %v", overrideDir, err)
	}
	const overrideContent = "root = true\n# ENV RELATIVE OVERRIDE\n"
	if err := os.WriteFile(filepath.Join(overrideDir, ".editorconfig.tpl"), []byte(overrideContent), 0o644); err != nil { //nolint:gosec // test fixture file
		t.Fatalf("WriteFile override failed: %v", err)
	}

	invocationDir := filepath.Join(work, "invocation")
	if err := os.MkdirAll(invocationDir, 0o755); err != nil { //nolint:gosec // test fixture directory
		t.Fatalf("MkdirAll(%q) failed: %v", invocationDir, err)
	}
	t.Chdir(invocationDir)
	t.Setenv("SCBAKE_TEMPLATE_DIR", "../ovr")

	const project = "env-relative"
	out, err := runCLI("new", project, "--with", "editorconfig")
	if err != nil {
		t.Fatalf("new with relative SCBAKE_TEMPLATE_DIR failed: %v\n%s", err, out)
	}

	content, err := os.ReadFile(filepath.Join(invocationDir, project, ".editorconfig")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("reading generated .editorconfig failed: %v", err)
	}
	if !strings.Contains(string(content), "ENV RELATIVE OVERRIDE") {
		t.Errorf(".editorconfig did not use the relative SCBAKE_TEMPLATE_DIR, got:\n%s", string(content))
	}
}

// TestTemplateDir_StaleEnvDoesNotBreakNonMutatingCommands guards the scope of
// #141: validating the override directory must not affect commands that never
// consume templates.
func TestTemplateDir_StaleEnvDoesNotBreakNonMutatingCommands(t *testing.T) {
	isolateUserDirs(t)
	t.Setenv("SCBAKE_TEMPLATE_DIR", "/nope")

	cases := [][]string{
		{"--version"},
		{"template", "registry", "list"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			out, err := runCLI(args...)
			if err != nil {
				t.Fatalf("scbake %s failed with a stale SCBAKE_TEMPLATE_DIR: %v\n%s", strings.Join(args, " "), err, out)
			}
		})
	}
}

// TestTemplateDir_UpdateAndRepairRejectMissingDir covers the two additional
// call sites that consume the override directory: a stale path must fail
// update and drift --repair before reconciliation, while check-only drift
// remains independent of the override.
func TestTemplateDir_UpdateAndRepairRejectMissingDir(t *testing.T) {
	isolateUserDirs(t)
	work := t.TempDir()
	t.Chdir(work)
	// Clear an ambient override so project setup is not affected by it.
	t.Setenv("SCBAKE_TEMPLATE_DIR", "")

	if out, err := runCLI("new", "proj", "--with", "editorconfig"); err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	projectDir := filepath.Join(work, "proj")
	t.Chdir(projectDir)
	t.Setenv("SCBAKE_TEMPLATE_DIR", filepath.Join(work, "missing-override"))

	for _, args := range [][]string{{"update"}, {"drift", "--repair"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			out, err := runCLI(args...)
			if err == nil {
				t.Fatalf("scbake %s succeeded with a missing SCBAKE_TEMPLATE_DIR:\n%s", strings.Join(args, " "), out)
			}
			if !strings.Contains(out, "missing-override") {
				t.Errorf("scbake %s error does not name the missing override directory:\n%s", strings.Join(args, " "), out)
			}
		})
	}

	if out, err := runCLI("drift"); err != nil {
		t.Fatalf("check-only drift must ignore the override directory: %v\n%s", err, out)
	}
}
