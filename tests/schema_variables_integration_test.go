// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// chdirTempWorkspace creates an isolated working directory and chdirs into it
// for the duration of the test.
func chdirTempWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	return dir
}

// hostGoDirective returns the version the host go toolchain writes into go.mod
// for `go mod init`, by running that command in a fresh temp directory and
// reading its `go` directive. `go env GOVERSION` is not equivalent: it can
// report suffixes such as X:boringcrypto or "devel" that `go mod init` does not
// write. The probe runs outside the test's cwd so it cannot pick up the
// repository's go.mod/toolchain directive or depend on when the test changes
// directory, and it does not touch the network.
func hostGoDirective(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "go", "mod", "init", "probe")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod init probe: %v\n%s", err, out)
	}
	goMod := readProjectFile(t, filepath.Join(dir, "go.mod"))
	for _, line := range strings.Split(goMod, "\n") {
		version, ok := strings.CutPrefix(line, "go ")
		if !ok {
			continue
		}
		version = strings.TrimSpace(version)
		if version == "" {
			t.Fatalf("go mod init probe wrote an empty go directive:\n%s", goMod)
		}
		return version
	}
	t.Fatalf("go mod init probe wrote no go directive:\n%s", goMod)
	return ""
}

func readProjectFile(t *testing.T, path string) string {
	t.Helper()
	//nolint:gosec // test-controlled temp path
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requireRegexp(t *testing.T, what, body string, re *regexp.Regexp) {
	t.Helper()
	if !re.MatchString(body) {
		t.Fatalf("%s does not match %s:\n%s", what, re.String(), body)
	}
}

func requireNoRegexp(t *testing.T, what, body string, re *regexp.Regexp) {
	t.Helper()
	if re.MatchString(body) {
		t.Fatalf("%s unexpectedly matches %s:\n%s", what, re.String(), body)
	}
}

// TestIssue140_SchemaVariablesAreConsumed is the acceptance regression for
// #140: explicit --set values must reach the generated files.
func TestIssue140_SchemaVariablesAreConsumed(t *testing.T) {
	chdirTempWorkspace(t)

	modulePath := regexp.MustCompile(`(?m)^module github\.com/acme/gm$`)
	goVersion := regexp.MustCompile(`(?m)^go 1\.23$`)
	runnerOS := regexp.MustCompile(`(?m)^    runs-on: windows-latest$`)

	t.Run("module_path_and_go_version", func(t *testing.T) {
		out, err := runCLI("new", "gm", "--lang", "go",
			"--set", "module_path=github.com/acme/gm",
			"--set", "go_version=1.23")
		if err != nil {
			t.Fatalf("new failed: %v\n%s", err, out)
		}
		goMod := readProjectFile(t, filepath.Join("gm", "go.mod"))
		requireRegexp(t, "gm/go.mod", goMod, modulePath)
		requireRegexp(t, "gm/go.mod", goMod, goVersion)

		manifest := readProjectFile(t, filepath.Join("gm", "scbake.toml"))
		if !strings.Contains(manifest, `module_path = "github.com/acme/gm"`) {
			t.Errorf("manifest lost module_path:\n%s", manifest)
		}
		if !strings.Contains(manifest, `go_version = "1.23"`) {
			t.Errorf("manifest lost go_version:\n%s", manifest)
		}
	})

	t.Run("runner_os_windows", func(t *testing.T) {
		out, err := runCLI("new", "gmci", "--with", "ci_github",
			"--set", "runner_os=windows-latest")
		if err != nil {
			t.Fatalf("new failed: %v\n%s", err, out)
		}
		workflow := readProjectFile(t, filepath.Join("gmci", ".github", "workflows", "main.yml"))
		requireRegexp(t, "main.yml", workflow, runnerOS)
	})

	t.Run("runner_os_macos", func(t *testing.T) {
		out, err := runCLI("new", "gmci-mac", "--with", "ci_github",
			"--set", "runner_os=macos-latest")
		if err != nil {
			t.Fatalf("new failed: %v\n%s", err, out)
		}
		workflow := readProjectFile(t, filepath.Join("gmci-mac", ".github", "workflows", "main.yml"))
		requireRegexp(t, "main.yml", workflow, regexp.MustCompile(`(?m)^    runs-on: macos-latest$`))
	})
}

// TestIssue224_CIGitHubRejectsInvalidGoVersion verifies the ci_github schema
// rejects a malformed pin instead of rendering an invalid workflow.
func TestIssue224_CIGitHubRejectsInvalidGoVersion(t *testing.T) {
	chdirTempWorkspace(t)

	out, err := runCLI("new", "badci", "--with", "ci_github", "--set", "go_version=garbage")
	if err == nil {
		t.Fatalf("new with invalid go_version should fail:\n%s", out)
	}
	if !strings.Contains(out, "go_version") || !strings.Contains(out, "pattern") {
		t.Errorf("error should identify go_version and its pattern:\n%s", out)
	}
	if _, statErr := os.Stat("badci"); !os.IsNotExist(statErr) {
		t.Errorf("failed project directory should be removed, stat err: %v", statErr)
	}
}

// TestIssue140_PlainGoProjectDoesNotApplySchemaDefault is the critical trap:
// a project that did not ask for a version must not inherit a schema default
// go_version, its module name must stay directory-derived, and go.mod must keep
// the host toolchain version.
func TestIssue140_PlainGoProjectDoesNotApplySchemaDefault(t *testing.T) {
	chdirTempWorkspace(t)

	out, err := runCLI("new", "plain", "--lang", "go")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	goMod := readProjectFile(t, filepath.Join("plain", "go.mod"))
	requireRegexp(t, "plain/go.mod", goMod, regexp.MustCompile(`(?m)^module plain$`))
	requireRegexp(t, "plain/go.mod", goMod,
		regexp.MustCompile(`(?m)^go `+regexp.QuoteMeta(hostGoDirective(t))+`$`))
	requireNoRegexp(t, "plain/go.mod", goMod, regexp.MustCompile(`(?m)^go 1\.22$`))

	// Without a schema default the plain project records no go_version; CI
	// reads the directive from go.mod instead of a duplicated value.
	manifest := readProjectFile(t, filepath.Join("plain", "scbake.toml"))
	if strings.Contains(manifest, "go_version") {
		t.Errorf("plain project must not record a go_version default:\n%s", manifest)
	}

	out, err = runCLI("new", "plain-dry", "--lang", "go", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "go mod edit") {
		t.Errorf("plain dry-run must not schedule a go directive edit:\n%s", out)
	}
	if _, statErr := os.Stat("plain-dry"); !os.IsNotExist(statErr) {
		t.Errorf("dry-run created the project directory: %v", statErr)
	}
}

// TestIssue140_EmptySetValuesAreUnset verifies that explicit empty values do
// not create an edit task or override the derived module name.
func TestIssue140_EmptySetValuesAreUnset(t *testing.T) {
	chdirTempWorkspace(t)

	out, err := runCLI("new", "e", "--lang", "go",
		"--set", "module_path=", "--set", "go_version=")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	goMod := readProjectFile(t, filepath.Join("e", "go.mod"))
	requireRegexp(t, "e/go.mod", goMod, regexp.MustCompile(`(?m)^module e$`))
	requireNoRegexp(t, "e/go.mod", goMod, regexp.MustCompile(`(?m)^go 1\.22$`))

	out, err = runCLI("new", "e-dry", "--lang", "go", "--dry-run",
		"--set", "module_path=", "--set", "go_version=")
	if err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "go mod edit") {
		t.Errorf("empty values must not schedule a go directive edit:\n%s", out)
	}
}

// TestIssue140_DryRunPreviewsValues verifies the requested values are visible
// in the plan preview and that no directory is created.
func TestIssue140_DryRunPreviewsValues(t *testing.T) {
	chdirTempWorkspace(t)

	out, err := runCLI("new", "dry", "--lang", "go", "--dry-run",
		"--set", "module_path=github.com/acme/gm",
		"--set", "go_version=1.23")
	if err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"go mod init github.com/acme/gm",
		"go mod edit -go=1.23",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if _, statErr := os.Stat("dry"); !os.IsNotExist(statErr) {
		t.Errorf("dry-run created the project directory: %v", statErr)
	}
}

// TestIssue140_ApplyGoVersionEditsExistingProject covers the maintenance path.
func TestIssue140_ApplyGoVersionEditsExistingProject(t *testing.T) {
	chdirTempWorkspace(t)

	if out, err := runCLI("new", "ap", "--lang", "go"); err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	project := filepath.Join(".", "ap")
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir project: %v", err)
	}

	out, err := runCLI("apply", "--lang", "go", "--set", "go_version=1.23")
	if err != nil {
		t.Fatalf("apply failed: %v\n%s", err, out)
	}

	goMod := readProjectFile(t, "go.mod")
	requireRegexp(t, "go.mod", goMod, regexp.MustCompile(`(?m)^module ap$`))
	requireRegexp(t, "go.mod", goMod, regexp.MustCompile(`(?m)^go 1\.23$`))
	manifest := readProjectFile(t, "scbake.toml")
	if !strings.Contains(manifest, `go_version = "1.23"`) {
		t.Errorf("manifest does not record the applied version:\n%s", manifest)
	}
}

// setupGoDependencyProject creates ../dep (go 1.24.0) and a Go project whose
// go.mod also declares go 1.24.0 and requires the dependency, then chdirs into
// the project.
func setupGoDependencyProject(t *testing.T, workspace, name string) {
	t.Helper()
	depDir := filepath.Join(workspace, "dep")
	if err := os.Mkdir(depDir, 0o700); err != nil {
		t.Fatalf("create dependency dir: %v", err)
	}
	writeTestFile(t, filepath.Join(depDir, "go.mod"), "module example.com/dep\n\ngo 1.24.0\n")
	writeTestFile(t, filepath.Join(depDir, "dep.go"), "package dep\n\nfunc Value() int { return 1 }\n")

	if out, err := runCLI("new", name, "--lang", "go"); err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	if err := os.Chdir(filepath.Join(workspace, name)); err != nil {
		t.Fatalf("chdir project: %v", err)
	}

	writeTestFile(t, "go.mod", "module "+name+"\n\ngo 1.24.0\n\n"+
		"require example.com/dep v0.0.0\n\nreplace example.com/dep => ../dep\n")
	writeTestFile(t, "main.go", "package main\n\nimport \"example.com/dep\"\n\nfunc main() { _ = dep.Value() }\n")
}

// TestIssue140_ExplicitGoVersionCannotContradictDependency pins the fail-closed
// path: `go mod tidy -go=<requested>` rejects a version below a dependency's
// directive, the run fails before metadata is saved, and rollback restores the
// original go.mod.
func TestIssue140_ExplicitGoVersionCannotContradictDependency(t *testing.T) {
	workspace := chdirTempWorkspace(t)
	setupGoDependencyProject(t, workspace, "gvfail")

	before := readProjectFile(t, "go.mod")
	manifestBefore := readProjectFile(t, "scbake.toml")

	out, err := runCLI("apply", "--lang", "go", "--set", "go_version=1.23",
		"--conflict-strategy", "keep-local")
	if err == nil {
		t.Fatalf("apply with go_version=1.23 should fail against a go 1.24 dependency:\n%s", out)
	}
	for _, want := range []string{"requires go@1.24.0", "1.23 is requested"} {
		if !strings.Contains(out, want) {
			t.Errorf("failure should mention %q:\n%s", want, out)
		}
	}
	if after := readProjectFile(t, "go.mod"); after != before {
		t.Errorf("rollback did not restore go.mod:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	if after := readProjectFile(t, "scbake.toml"); after != manifestBefore {
		t.Errorf("failed run changed the manifest:\n--- before ---\n%s\n--- after ---\n%s", manifestBefore, after)
	}
}

// TestIssue140_ExplicitPatchGoVersionMatchesDependency verifies the schema
// accepts the patch-level form needed to satisfy a `go 1.24.0` dependency.
func TestIssue140_ExplicitPatchGoVersionMatchesDependency(t *testing.T) {
	workspace := chdirTempWorkspace(t)
	setupGoDependencyProject(t, workspace, "gvpatch")

	out, err := runCLI("apply", "--lang", "go", "--set", "go_version=1.24.0",
		"--conflict-strategy", "keep-local")
	if err != nil {
		t.Fatalf("apply with go_version=1.24.0 failed: %v\n%s", err, out)
	}
	requireRegexp(t, "go.mod", readProjectFile(t, "go.mod"), regexp.MustCompile(`(?m)^go 1\.24\.0$`))
	manifest := readProjectFile(t, "scbake.toml")
	if !strings.Contains(manifest, `go_version = "1.24.0"`) {
		t.Errorf("manifest does not record the patch-level version:\n%s", manifest)
	}
}

// TestIssue140_ApplyModulePathMismatchFailsClosed verifies an existing module
// path is never rewritten or falsely recorded.
func TestIssue140_ApplyModulePathMismatchFailsClosed(t *testing.T) {
	chdirTempWorkspace(t)

	if out, err := runCLI("new", "ap2", "--lang", "go"); err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	if err := os.Chdir(filepath.Join(".", "ap2")); err != nil {
		t.Fatalf("chdir project: %v", err)
	}

	out, err := runCLI("apply", "--lang", "go", "--set", "module_path=github.com/acme/other")
	if err == nil {
		t.Fatalf("expected apply with a differing module_path to fail:\n%s", out)
	}
	if !strings.Contains(out, "will not rewrite") {
		t.Errorf("error should explain that existing modules are not rewritten:\n%s", out)
	}
	requireRegexp(t, "go.mod", readProjectFile(t, "go.mod"), regexp.MustCompile(`(?m)^module ap2$`))
	if manifest := readProjectFile(t, "scbake.toml"); strings.Contains(manifest, "github.com/acme/other") {
		t.Errorf("unhonoured module_path was recorded:\n%s", manifest)
	}
}

// TestIssue140_ApplyEqualModulePathIsNoOp verifies idempotent scripts keep
// working: an explicit value equal to the existing module is accepted.
func TestIssue140_ApplyEqualModulePathIsNoOp(t *testing.T) {
	chdirTempWorkspace(t)

	if out, err := runCLI("new", "ap3", "--lang", "go"); err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	if err := os.Chdir(filepath.Join(".", "ap3")); err != nil {
		t.Fatalf("chdir project: %v", err)
	}

	out, err := runCLI("apply", "--lang", "go", "--set", "module_path=ap3")
	if err != nil {
		t.Fatalf("equal module_path should be a no-op: %v\n%s", err, out)
	}
	requireRegexp(t, "go.mod", readProjectFile(t, "go.mod"), regexp.MustCompile(`(?m)^module ap3$`))
}

// TestIssue140_InvalidModulePathFailsClosed verifies plan-time rejection and
// cleanup for flag-like or whitespace module paths.
func TestIssue140_InvalidModulePathFailsClosed(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "leading_dash", value: "-x", want: "leading dash"},
		{name: "leading_double_dash", value: "--", want: "leading dash"},
		{name: "internal_space", value: "github.com/acme gm", want: "whitespace"},
		{name: "leading_space", value: " github.com/acme/gm", want: "whitespace"},
		{name: "trailing_space", value: "github.com/acme/gm ", want: "whitespace"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chdirTempWorkspace(t)
			project := "badmod"
			out, err := runCLI("new", project, "--lang", "go", "--set", "module_path="+tc.value)
			if err == nil {
				t.Fatalf("expected new to fail for module_path=%q:\n%s", tc.value, out)
			}
			if !strings.Contains(out, "module_path") || !strings.Contains(out, tc.want) {
				t.Errorf("error should mention module_path and %q:\n%s", tc.want, out)
			}
			if _, statErr := os.Stat(project); !os.IsNotExist(statErr) {
				t.Errorf("failed project directory should be removed, stat err: %v", statErr)
			}
		})
	}
}

// TestIssue140_UpdateWithNoVarsIsNoOp ensures the update path (which passes
// nil vars) never replays the directive edit.
func TestIssue140_UpdateWithNoVarsIsNoOp(t *testing.T) {
	chdirTempWorkspace(t)

	if out, err := runCLI("new", "up", "--lang", "go"); err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	if err := os.Chdir(filepath.Join(".", "up")); err != nil {
		t.Fatalf("chdir project: %v", err)
	}
	before := readProjectFile(t, "go.mod")

	if out, err := runCLI("update", "--dry-run"); err != nil {
		t.Fatalf("update dry-run failed: %v\n%s", err, out)
	} else if strings.Contains(out, "go mod edit") {
		t.Errorf("update must not schedule a go directive edit:\n%s", out)
	}

	if out, err := runCLI("update"); err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}
	if after := readProjectFile(t, "go.mod"); after != before {
		t.Errorf("update rewrote go.mod without vars:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}
