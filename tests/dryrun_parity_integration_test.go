// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dryRunExitCode maps the error returned by runCLI to the CLI process exit
// code. The CLI maps every command error to 1, but reading the real process
// status keeps this harness honest.
func dryRunExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// dryRunErrorLine extracts Cobra's single "Error: ..." line so dry and real
// runs can be compared without their progress lines or usage blocks.
func dryRunErrorLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "Error: ") {
			return line
		}
	}
	return ""
}

// dryRunSnapshot records file bytes, permissions, and link targets for every
// entry under root, hidden files included.
type dryRunSnapshot map[string]string

func dryRunTakeSnapshot(t *testing.T, root string) dryRunSnapshot {
	t.Helper()
	snap := make(dryRunSnapshot)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snap[rel] = "l " + target
			return nil
		}
		if entry.IsDir() {
			snap[rel] = "d " + info.Mode().Perm().String()
			return nil
		}
		data, err := os.ReadFile(filepath.Clean(path)) // #nosec G304,G122 -- walking a test temp dir
		if err != nil {
			return err
		}
		snap[rel] = fmt.Sprintf("f %s %x", info.Mode().Perm(), data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", root, err)
	}
	return snap
}

func dryRunAssertSnapshotsEqual(t *testing.T, before, after dryRunSnapshot) {
	t.Helper()
	for path, want := range before {
		got, ok := after[path]
		if !ok {
			t.Errorf("dry run removed %q", path)
			continue
		}
		if got != want {
			t.Errorf("dry run changed %q:\nbefore: %s\nafter:  %s", path, want, got)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("dry run created %q", path)
		}
	}
}

// dryRunAssertNoResidue fails when a dry run leaves transaction state, an
// artifact, or a lock behind. Pre-existing .scbake-base content is fine; a
// transaction directory is not.
func dryRunAssertNoResidue(t *testing.T, snap dryRunSnapshot) {
	t.Helper()
	for path := range snap {
		for _, part := range strings.Split(path, "/") {
			if part == ".scbake" {
				t.Errorf("dry run left transaction state at %q", path)
			}
		}
		if strings.HasSuffix(path, ".scbake-new") {
			t.Errorf("dry run left an artifact at %q", path)
		}
	}
}

func dryRunWriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("creating parent for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("writing %s: %v", rel, err)
	}
}

func dryRunRunIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	return runCLI(args...)
}

func dryRunAssertSameFailure(t *testing.T, dryOut string, dryErr error, realOut string, realErr error, wantDescription string) {
	t.Helper()
	dryCode := dryRunExitCode(dryErr)
	realCode := dryRunExitCode(realErr)
	if dryCode != realCode {
		t.Fatalf("dry-run exit = %d, real exit = %d\n--- dry output ---\n%s\n--- real output ---\n%s", dryCode, realCode, dryOut, realOut)
	}
	if dryCode != 1 {
		t.Fatalf("exit = %d, want 1 for a failing fixture\n--- dry output ---\n%s\n--- real output ---\n%s", dryCode, dryOut, realOut)
	}
	dryLine := dryRunErrorLine(dryOut)
	realLine := dryRunErrorLine(realOut)
	if dryLine == "" || realLine == "" {
		t.Fatalf("missing Error line\ndry:  %q\nreal: %q\n--- dry output ---\n%s\n--- real output ---\n%s", dryLine, realLine, dryOut, realOut)
	}
	if dryLine != realLine {
		t.Errorf("dry-run error differs from real run:\ndry:  %s\nreal: %s", dryLine, realLine)
	}
	want := "task failed (" + wantDescription + "):"
	if wantDescription != "" && !strings.Contains(dryLine, want) {
		t.Errorf("dry-run error = %q, want the first failing task %q", dryLine, want)
	}
}

// --- Regression 1: apply-time conflicts and validation failures ---------------

func TestDryRunParityEditorconfigConflict(t *testing.T) {
	prepare := func(dir string) {
		dryRunWriteFile(t, dir, ".editorconfig", "root = true\n")
	}

	dryDir := t.TempDir()
	prepare(dryDir)
	before := dryRunTakeSnapshot(t, dryDir)
	dryOut, dryErr := dryRunRunIn(t, dryDir, "apply", "--with", "editorconfig", "--dry-run", "--conflict-strategy=fail")
	dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, dryDir))
	dryRunAssertNoResidue(t, dryRunTakeSnapshot(t, dryDir))

	realDir := t.TempDir()
	prepare(realDir)
	realOut, realErr := dryRunRunIn(t, realDir, "apply", "--with", "editorconfig", "--conflict-strategy=fail")

	dryRunAssertSameFailure(t, dryOut, dryErr, realOut, realErr, "Create standardized .editorconfig")
	if !strings.Contains(dryRunErrorLine(dryOut), "manual modifications") {
		t.Errorf("dry-run error = %q, want a drift conflict", dryRunErrorLine(dryOut))
	}
}

func TestDryRunParityComplianceMissingMetadata(t *testing.T) {
	args := []string{"apply", "--with", "compliance", "--dry-run", "--conflict-strategy=fail"}
	dryDir := t.TempDir()
	before := dryRunTakeSnapshot(t, dryDir)
	dryOut, dryErr := dryRunRunIn(t, dryDir, args...)
	dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, dryDir))

	realArgs := []string{"apply", "--with", "compliance", "--conflict-strategy=fail"}
	realDir := t.TempDir()
	realOut, realErr := dryRunRunIn(t, realDir, realArgs...)

	dryRunAssertSameFailure(t, dryOut, dryErr, realOut, realErr, "Generate LICENSE file")
	if !strings.Contains(dryRunErrorLine(dryOut), "copyright_holder") {
		t.Errorf("dry-run error = %q, want the compliance metadata error", dryRunErrorLine(dryOut))
	}
}

func TestDryRunParityMavenTargetErrors(t *testing.T) {
	cases := []struct {
		name    string
		pom     string
		write   bool
		wantSub string
	}{
		{name: "missing pom", write: false, wantSub: "XML file not found: pom.xml"},
		{name: "malformed pom", pom: "<project><build>\n", write: true, wantSub: "malformed"},
		{name: "no insertion point", pom: "<project><build></build></project>", write: true, wantSub: "target element path /project/build/plugins not found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepare := func(dir string) {
				if tc.write {
					dryRunWriteFile(t, dir, "pom.xml", tc.pom)
				}
			}

			dryDir := t.TempDir()
			prepare(dryDir)
			before := dryRunTakeSnapshot(t, dryDir)
			dryOut, dryErr := dryRunRunIn(t, dryDir, "apply", "--with", "maven_linter", "--dry-run", "--conflict-strategy=fail")
			after := dryRunTakeSnapshot(t, dryDir)
			dryRunAssertSnapshotsEqual(t, before, after)
			// No earlier task produces pom.xml, so the failing dry run must not
			// leave transaction state or an artifact behind either.
			dryRunAssertNoResidue(t, after)

			realDir := t.TempDir()
			prepare(realDir)
			realOut, realErr := dryRunRunIn(t, realDir, "apply", "--with", "maven_linter", "--conflict-strategy=fail")

			dryRunAssertSameFailure(t, dryOut, dryErr, realOut, realErr, "Inject Maven Checkstyle plugin into pom.xml")
			if !strings.Contains(dryRunErrorLine(dryOut), tc.wantSub) {
				t.Errorf("dry-run error = %q, want substring %q", dryRunErrorLine(dryOut), tc.wantSub)
			}
		})
	}
}

func dryRunSetupManagedEdit(t *testing.T, dir string) string {
	t.Helper()
	if out, err := dryRunRunIn(t, dir, "new", "proj", "--with", "makefile,editorconfig"); err != nil {
		t.Fatalf("preparing update fixture: %v\n%s", err, out)
	}
	project := filepath.Join(dir, "proj")
	editorconfig := filepath.Join(project, ".editorconfig")
	data, err := os.ReadFile(filepath.Clean(editorconfig)) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatalf("reading managed .editorconfig: %v", err)
	}
	if err := os.WriteFile(editorconfig, append(data, []byte("\n# local edit\n")...), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("editing managed .editorconfig: %v", err)
	}
	return project
}

func TestDryRunParityUpdateConflict(t *testing.T) {
	dryDir := t.TempDir()
	dryProject := dryRunSetupManagedEdit(t, dryDir)
	before := dryRunTakeSnapshot(t, dryProject)
	dryOut, dryErr := dryRunRunIn(t, dryProject, "update", "--conflict-strategy=fail", "--dry-run")
	dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, dryProject))

	realDir := t.TempDir()
	realProject := dryRunSetupManagedEdit(t, realDir)
	realOut, realErr := dryRunRunIn(t, realProject, "update", "--conflict-strategy=fail")

	dryRunAssertSameFailure(t, dryOut, dryErr, realOut, realErr, "Create standardized .editorconfig")
}

func TestDryRunParityDriftRepairConflict(t *testing.T) {
	dryDir := t.TempDir()
	dryProject := dryRunSetupManagedEdit(t, dryDir)
	before := dryRunTakeSnapshot(t, dryProject)
	dryOut, dryErr := dryRunRunIn(t, dryProject, "drift", "--repair", "--conflict-strategy=fail", "--dry-run")
	dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, dryProject))

	realDir := t.TempDir()
	realProject := dryRunSetupManagedEdit(t, realDir)
	realOut, realErr := dryRunRunIn(t, realProject, "drift", "--repair", "--conflict-strategy=fail")

	dryRunAssertSameFailure(t, dryOut, dryErr, realOut, realErr, "Create standardized .editorconfig")
}

// --- Regression 2: dry runs must not write ------------------------------------

func TestDryRunValidApplyWritesNothing(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, dir string)
		args    []string
	}{
		{
			name: "valid compliance",
			args: []string{"apply", "--with", "compliance", "--license", "MIT", "--copyright-holder", "Test Corp", "--dry-run"},
		},
		{
			name: "valid editorconfig",
			args: []string{"apply", "--with", "editorconfig", "--dry-run"},
		},
		{
			name: "valid community",
			args: []string{"apply", "--with", "community", "--dry-run"},
		},
		{
			name: "valid maven",
			prepare: func(t *testing.T, dir string) {
				dryRunWriteFile(t, dir, "pom.xml", "<project><build><plugins></plugins></build></project>")
			},
			args: []string{"apply", "--with", "maven_linter", "--dry-run"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.prepare != nil {
				tc.prepare(t, dir)
			}
			before := dryRunTakeSnapshot(t, dir)
			out, err := dryRunRunIn(t, dir, tc.args...)
			if err != nil {
				t.Fatalf("dry run = %v, want nil\n%s", err, out)
			}
			after := dryRunTakeSnapshot(t, dir)
			dryRunAssertSnapshotsEqual(t, before, after)
			dryRunAssertNoResidue(t, after)
			for _, unwanted := range []string{"scbake.toml", ".editorconfig", "LICENSE", "Makefile", ".checkstyle.xml"} {
				if _, ok := after[unwanted]; ok && before[unwanted] == "" {
					t.Errorf("dry run created %q", unwanted)
				}
			}
		})
	}
}

// --- Regression 3: root anchor / subproject and multi-project parity ----------

func dryRunSetupSubproject(t *testing.T, dir string) string {
	t.Helper()
	if out, err := dryRunRunIn(t, dir, "new", "repo", "--with", "makefile,git"); err != nil {
		t.Fatalf("preparing monorepo root: %v\n%s", err, out)
	}
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(filepath.Join(repo, "svc"), 0o750); err != nil {
		t.Fatalf("creating svc: %v", err)
	}
	if out, err := dryRunRunIn(t, repo, "apply", "--with", "editorconfig", "svc"); err != nil {
		t.Fatalf("applying editorconfig to svc: %v\n%s", err, out)
	}
	return repo
}

// TestDryRunConflictStrategiesWriteNothing covers the strategies that resolve a
// conflict without failing: a preview may print the artifact/keep-local wording,
// but it must not create the artifact or touch the managed file.
func TestDryRunConflictStrategiesWriteNothing(t *testing.T) {
	for _, strategy := range []string{"artifact", "keep-local", "merge", "overwrite"} {
		t.Run(strategy, func(t *testing.T) {
			dir := t.TempDir()
			project := dryRunSetupManagedEdit(t, dir)
			before := dryRunTakeSnapshot(t, project)

			out, err := dryRunRunIn(t, project, "apply", "--with", "editorconfig", "--conflict-strategy="+strategy, "--dry-run")
			if err != nil {
				t.Fatalf("dry run = %v, want nil for strategy %s\n%s", err, strategy, out)
			}
			after := dryRunTakeSnapshot(t, project)
			dryRunAssertSnapshotsEqual(t, before, after)
			dryRunAssertNoResidue(t, after)
		})
	}
}

func TestDryRunRepeatAfterRealApply(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, dir string)
		args    []string
	}{
		{
			name: "editorconfig",
			args: []string{"apply", "--with", "editorconfig"},
		},
		{
			name: "compliance",
			args: []string{"apply", "--with", "compliance", "--license", "MIT", "--copyright-holder", "Test Corp"},
		},
		{
			name: "community",
			args: []string{"apply", "--with", "community"},
		},
		{
			name: "maven",
			prepare: func(t *testing.T, dir string) {
				dryRunWriteFile(t, dir, "pom.xml", "<project><build><plugins></plugins></build></project>")
			},
			args: []string{"apply", "--with", "maven_linter"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.prepare != nil {
				tc.prepare(t, dir)
			}
			if out, err := dryRunRunIn(t, dir, tc.args...); err != nil {
				t.Fatalf("real run = %v, want nil\n%s", err, out)
			}
			before := dryRunTakeSnapshot(t, dir)
			dryArgs := append(append([]string{}, tc.args...), "--dry-run")
			out, err := dryRunRunIn(t, dir, dryArgs...)
			if err != nil {
				t.Fatalf("repeat dry run = %v, want nil (false conflict)\n%s", err, out)
			}
			dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, dir))
			dryRunAssertNoResidue(t, dryRunTakeSnapshot(t, dir))
		})
	}
}

func TestDryRunRootAnchorSubprojectManagedFile(t *testing.T) {
	repo := dryRunSetupSubproject(t, t.TempDir())
	manifest, err := os.ReadFile(filepath.Join(repo, "scbake.toml")) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatalf("reading root manifest: %v", err)
	}
	if !strings.Contains(string(manifest), `"svc/.editorconfig"`) {
		t.Fatalf("fixture is not anchored at the root manifest:\n%s", manifest)
	}

	before := dryRunTakeSnapshot(t, repo)
	for i := 0; i < 2; i++ {
		out, err := dryRunRunIn(t, repo, "apply", "--with", "editorconfig", "svc", "--dry-run")
		if err != nil {
			t.Fatalf("dry run %d from root = %v, want nil (false conflict)\n%s", i+1, err, out)
		}
		if strings.Contains(out, "manual modifications") {
			t.Errorf("dry run %d from root reported a false conflict:\n%s", i+1, out)
		}
	}

	if out, err := dryRunRunIn(t, filepath.Join(repo, "svc"), "apply", "--with", "editorconfig", "--dry-run"); err != nil {
		t.Fatalf("dry run from inside the subproject = %v, want nil (false conflict)\n%s", err, out)
	}
	if out, err := dryRunRunIn(t, filepath.Join(repo, "svc"), "update", "--dry-run"); err != nil {
		t.Fatalf("update dry run from inside the subproject = %v, want nil (false conflict)\n%s", err, out)
	}
	dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, repo))
	dryRunAssertNoResidue(t, dryRunTakeSnapshot(t, repo))
}

func dryRunSetupMultiProject(t *testing.T, dir string) string {
	t.Helper()
	if out, err := dryRunRunIn(t, dir, "new", "repo", "--with", "makefile,git"); err != nil {
		t.Fatalf("preparing monorepo root: %v\n%s", err, out)
	}
	repo := filepath.Join(dir, "repo")
	if out, err := dryRunRunIn(t, repo, "apply", "--lang", "go", "api"); err != nil {
		t.Fatalf("adding api project: %v\n%s", err, out)
	}
	if out, err := dryRunRunIn(t, repo, "apply", "--lang", "go", "web"); err != nil {
		t.Fatalf("adding web project: %v\n%s", err, out)
	}
	if out, err := dryRunRunIn(t, repo, "apply", "--with", "community", "web"); err != nil {
		t.Fatalf("applying community to web: %v\n%s", err, out)
	}
	return repo
}

func TestDryRunRootAnchorMultiProjectRender(t *testing.T) {
	repo := dryRunSetupMultiProject(t, t.TempDir())

	contributing, err := os.ReadFile(filepath.Join(repo, "web", "CONTRIBUTING.md")) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatalf("reading web/CONTRIBUTING.md: %v", err)
	}
	if !strings.Contains(string(contributing), "Contributing to web") {
		t.Fatalf("real community render did not name the owning project web:\n%s", contributing)
	}
	if strings.Contains(string(contributing), "Contributing to api") {
		t.Fatalf("real community render named the wrong project:\n%s", contributing)
	}

	before := dryRunTakeSnapshot(t, repo)
	for i := 0; i < 2; i++ {
		out, err := dryRunRunIn(t, repo, "apply", "--with", "community", "web", "--dry-run")
		if err != nil {
			t.Fatalf("dry run %d = %v, want nil (false conflict)\n%s", i+1, err, out)
		}
		if !strings.Contains(out, "Skip CONTRIBUTING.md") {
			t.Errorf("dry run %d did not report the community plan:\n%s", i+1, out)
		}
		if strings.Contains(out, "manual modifications") {
			t.Errorf("dry run %d reported a false conflict:\n%s", i+1, out)
		}
	}
	dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, repo))
	dryRunAssertNoResidue(t, dryRunTakeSnapshot(t, repo))
}

// --- Regression 4: `new` and `apply` dry-run parity matrix --------------------

func TestDryRunNewWritesNothing(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "go", args: []string{"new", "p", "--lang", "go"}},
		{name: "spring", args: []string{"new", "p", "--lang", "spring"}},
		{name: "svelte", args: []string{"new", "p", "--lang", "svelte"}},
		{
			name: "compliance and community",
			args: []string{"new", "p", "--with", "compliance,community", "--license", "MIT", "--copyright-holder", "Test Corp"},
		},
		{name: "makefile", args: []string{"new", "p", "--with", "makefile"}},
		{name: "git", args: []string{"new", "p", "--with", "git"}},
		{
			name: "combined",
			args: []string{"new", "p", "--lang", "go", "--with", "compliance,community,makefile,git", "--license", "MIT", "--copyright-holder", "Test Corp"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			before := dryRunTakeSnapshot(t, dir)
			args := append(append([]string{}, tc.args...), "--dry-run")
			out, err := dryRunRunIn(t, dir, args...)
			if err != nil {
				t.Fatalf("new --dry-run = %v, want nil\n%s", err, out)
			}
			after := dryRunTakeSnapshot(t, dir)
			dryRunAssertSnapshotsEqual(t, before, after)
			dryRunAssertNoResidue(t, after)
			if _, statErr := os.Stat(filepath.Join(dir, "p")); !os.IsNotExist(statErr) {
				t.Errorf("dry run created the project directory (stat err: %v)", statErr)
			}
		})
	}
}

func TestDryRunNewSuccessParity(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "go", args: []string{"new", "p", "--lang", "go"}},
		{
			name: "compliance and community",
			args: []string{"new", "p", "--with", "compliance,community", "--license", "MIT", "--copyright-holder", "Test Corp"},
		},
		{name: "makefile", args: []string{"new", "p", "--with", "makefile"}},
		{name: "git", args: []string{"new", "p", "--with", "git"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dryDir := t.TempDir()
			before := dryRunTakeSnapshot(t, dryDir)
			dryArgs := append(append([]string{}, tc.args...), "--dry-run")
			dryOut, dryErr := dryRunRunIn(t, dryDir, dryArgs...)
			if dryErr != nil {
				t.Fatalf("dry run = %v, want nil\n%s", dryErr, dryOut)
			}
			dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, dryDir))

			realDir := t.TempDir()
			realOut, realErr := dryRunRunIn(t, realDir, tc.args...)
			if realErr != nil {
				t.Fatalf("real run = %v, want nil\n%s", realErr, realOut)
			}
			if _, statErr := os.Stat(filepath.Join(realDir, "p")); statErr != nil {
				t.Errorf("real run did not create the project: %v", statErr)
			}
		})
	}
}

func TestDryRunNewFailureClassParity(t *testing.T) {
	t.Run("existing target", func(t *testing.T) {
		for _, lang := range []string{"go", "spring", "svelte"} {
			t.Run(lang, func(t *testing.T) {
				prepare := func(dir string) {
					if err := os.Mkdir(filepath.Join(dir, "p"), 0o750); err != nil {
						t.Fatalf("creating existing target: %v", err)
					}
					dryRunWriteFile(t, dir, "p/sentinel.txt", "keep")
				}

				dryDir := t.TempDir()
				prepare(dryDir)
				dryOut, dryErr := dryRunRunIn(t, dryDir, "new", "p", "--lang", lang, "--dry-run")

				realDir := t.TempDir()
				prepare(realDir)
				realOut, realErr := dryRunRunIn(t, realDir, "new", "p", "--lang", lang)

				dryRunAssertSameFailure(t, dryOut, dryErr, realOut, realErr, "")
				if content, err := os.ReadFile(filepath.Join(realDir, "p", "sentinel.txt")); err != nil || string(content) != "keep" { //nolint:gosec // test fixture
					t.Errorf("real run changed the existing target (content %q, err %v)", content, err)
				}
			})
		}
	})

	t.Run("compliance missing metadata", func(t *testing.T) {
		dryDir := t.TempDir()
		dryOut, dryErr := dryRunRunIn(t, dryDir, "new", "p", "--with", "compliance", "--copyright-holder", "Test Corp", "--dry-run")

		realDir := t.TempDir()
		realOut, realErr := dryRunRunIn(t, realDir, "new", "p", "--with", "compliance", "--copyright-holder", "Test Corp")

		dryRunAssertSameFailure(t, dryOut, dryErr, realOut, realErr, "Generate LICENSE file")
		if _, err := os.Stat(filepath.Join(realDir, "p")); !os.IsNotExist(err) {
			t.Errorf("real failed run left the project directory behind (stat err: %v)", err)
		}
	})
}

func TestDryRunApplySuccessParity(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, dir string)
		args    []string
	}{
		{
			name: "valid editorconfig",
			args: []string{"apply", "--with", "editorconfig"},
		},
		{
			name: "valid compliance",
			args: []string{"apply", "--with", "compliance", "--license", "MIT", "--copyright-holder", "Test Corp"},
		},
		{
			name: "valid community",
			args: []string{"apply", "--with", "community"},
		},
		{
			name: "valid maven",
			prepare: func(t *testing.T, dir string) {
				dryRunWriteFile(t, dir, "pom.xml", "<project><build><plugins></plugins></build></project>")
			},
			args: []string{"apply", "--with", "maven_linter"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dryDir := t.TempDir()
			if tc.prepare != nil {
				tc.prepare(t, dryDir)
			}
			before := dryRunTakeSnapshot(t, dryDir)
			dryArgs := append(append([]string{}, tc.args...), "--dry-run")
			dryOut, dryErr := dryRunRunIn(t, dryDir, dryArgs...)
			if dryErr != nil {
				t.Fatalf("dry run = %v, want nil\n%s", dryErr, dryOut)
			}
			dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, dryDir))

			realDir := t.TempDir()
			if tc.prepare != nil {
				tc.prepare(t, realDir)
			}
			realOut, realErr := dryRunRunIn(t, realDir, tc.args...)
			if realErr != nil {
				t.Fatalf("real run = %v, want nil\n%s", realErr, realOut)
			}
		})
	}
}

// TestDryRunSameFirstFailingTask pins that a later task failing first in both
// modes keeps the same description and cause. maven_linter and compliance are
// both selected; the Maven injection task runs first.
func TestDryRunSameFirstFailingTask(t *testing.T) {
	args := []string{"apply", "--with", "maven_linter,compliance", "--conflict-strategy=fail"}
	dryDir := t.TempDir()
	dryOut, dryErr := dryRunRunIn(t, dryDir, append(append([]string{}, args...), "--dry-run")...)

	realDir := t.TempDir()
	realOut, realErr := dryRunRunIn(t, realDir, args...)

	dryRunAssertSameFailure(t, dryOut, dryErr, realOut, realErr, "Inject Maven Checkstyle plugin into pom.xml")
	line := dryRunErrorLine(dryOut)
	if strings.Contains(line, "Generate LICENSE file") {
		t.Errorf("dry run reported the wrong first failing task: %s", line)
	}
}
