// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// newIdenticalFileFixture builds an unmanaged target that renders the simple
// template with the given manifest, returning the context, task, and canonical
// render. The manifest starts with no managed_files entry, so the target is an
// ordinary pre-existing file.
func newIdenticalFileFixture(t *testing.T, strategy string) (types.TaskContext, *CreateTemplateTask, []byte) {
	t.Helper()

	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	tc := types.TaskContext{
		Ctx:              context.Background(),
		TargetPath:       root,
		Manifest:         &types.Manifest{SbakeVersion: "v1.0.0", Projects: []types.Project{{Name: "Identical"}}},
		Tx:               tx,
		ConflictStrategy: strategy,
	}
	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "output.txt",
		Desc:         "Identical file",
		TaskPrio:     100,
	}

	rendered, err := task.render(tc)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return tc, task, rendered
}

// writeTarget stores content at the task's output path and freezes its mtime so
// an unnecessary rewrite is observable.
func writeTarget(t *testing.T, tc types.TaskContext, task *CreateTemplateTask, content []byte) string {
	t.Helper()

	path := filepath.Join(tc.TargetPath, task.OutputPath)
	if err := os.WriteFile(path, content, fileutil.FilePerms); err != nil {
		t.Fatalf("write target: %v", err)
	}
	freezeAcceptedFile(t, path)
	return path
}

// assertNotExist fails when path exists.
func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s exists (stat err: %v), want it absent", path, err)
	}
}

// TestCreateTemplateTaskAdoptsIdenticalUnmanagedFile is the #220 regression: a
// pre-existing file whose bytes already equal the render is adopted, not
// reported as manual drift, and it is not rewritten.
func TestCreateTemplateTaskAdoptsIdenticalUnmanagedFile(t *testing.T) {
	tc, task, rendered := newIdenticalFileFixture(t, types.ConflictFail)
	path := writeTarget(t, tc, task, rendered)

	var execErr error
	stderr := captureStderr(t, func() { execErr = task.Execute(tc) })
	if execErr != nil {
		t.Fatalf("Execute() on a byte-identical unmanaged file = %v, want nil", execErr)
	}
	if strings.Contains(stderr, "manual modifications") || strings.Contains(stderr, "Conflict") {
		t.Errorf("identical file reported as a conflict: %q", stderr)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != string(rendered) {
		t.Errorf("target bytes changed: %q, want %q", got, rendered)
	}
	assertAcceptedFileFrozen(t, path)
	assertNotExist(t, path+".scbake-new")

	if got := tc.Manifest.ManagedFiles[task.OutputPath]; got != HashContent(rendered) {
		t.Errorf("managed_files[%q] = %q, want %q", task.OutputPath, got, HashContent(rendered))
	}
	base, err := os.ReadFile(filepath.Join(tc.TargetPath, BaseShadowRelPath(task.OutputPath)))
	if err != nil {
		t.Fatalf("read pristine base: %v", err)
	}
	if string(base) != string(rendered) {
		t.Errorf("pristine base = %q, want %q", base, rendered)
	}
}

// TestCreateTemplateTaskIdenticalArtifactStrategyWritesNoArtifact: the artifact
// strategy must not create a byte-identical .scbake-new side file for a file
// that already matches the render.
func TestCreateTemplateTaskIdenticalArtifactStrategyWritesNoArtifact(t *testing.T) {
	tc, task, rendered := newIdenticalFileFixture(t, types.ConflictArtifact)
	path := writeTarget(t, tc, task, rendered)

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() with artifact strategy on identical bytes = %v, want nil", err)
	}
	assertAcceptedFileFrozen(t, path)
	assertNotExist(t, path+".scbake-new")
	if got := tc.Manifest.ManagedFiles[task.OutputPath]; got != HashContent(rendered) {
		t.Errorf("managed_files[%q] = %q, want %q", task.OutputPath, got, HashContent(rendered))
	}
}

// TestCreateTemplateTaskKeepLocalIdenticalFileIsAdopted: keep-local must not
// turn a byte-identical unmanaged file into a skipped conflict. The file is
// already what the template would write, so it is adopted and recorded.
func TestCreateTemplateTaskKeepLocalIdenticalFileIsAdopted(t *testing.T) {
	tc, task, rendered := newIdenticalFileFixture(t, types.ConflictKeepLocal)
	path := writeTarget(t, tc, task, rendered)

	var execErr error
	stderr := captureStderr(t, func() { execErr = task.Execute(tc) })
	if execErr != nil {
		t.Fatalf("Execute() with keep-local on identical bytes = %v, want nil", execErr)
	}
	if strings.Contains(stderr, "Conflict") || strings.Contains(stderr, "Skipping") {
		t.Errorf("identical file was skipped as a conflict: %q", stderr)
	}
	assertAcceptedFileFrozen(t, path)
	assertNotExist(t, path+".scbake-new")
	if got := tc.Manifest.ManagedFiles[task.OutputPath]; got != HashContent(rendered) {
		t.Errorf("managed_files[%q] = %q, want %q", task.OutputPath, got, HashContent(rendered))
	}
	base, err := os.ReadFile(filepath.Join(tc.TargetPath, BaseShadowRelPath(task.OutputPath)))
	if err != nil {
		t.Fatalf("read pristine base: %v", err)
	}
	if string(base) != string(rendered) {
		t.Errorf("pristine base = %q, want %q", base, rendered)
	}
}

// TestCreateTemplateTaskIdenticalFileWithNilManifest: the identical-file
// convergence path must not dereference a nil manifest (pkg/tasks is public).
// With no manifest there is no ledger or base to advance, so the dry-run half
// and the execute half both succeed without writing state.
func TestCreateTemplateTaskIdenticalFileWithNilManifest(t *testing.T) {
	tc, task, rendered := newIdenticalFileFixture(t, types.ConflictFail)
	path := writeTarget(t, tc, task, rendered)

	// The render above used a manifest; a direct library caller may execute the
	// same bytes with no manifest at all.
	tc.Manifest = nil
	tc.Tx = nil

	if err := ValidateManagedFile(tc, task.OutputPath, rendered); err != nil {
		t.Fatalf("ValidateManagedFile with nil manifest = %v, want nil", err)
	}
	tc.DryRun = true
	if err := WriteManagedFile(tc, task.OutputPath, rendered, fileutil.FilePerms); err != nil {
		t.Fatalf("dry-run WriteManagedFile with nil manifest = %v, want nil", err)
	}
	tc.DryRun = false
	if err := WriteManagedFile(tc, task.OutputPath, rendered, fileutil.FilePerms); err != nil {
		t.Fatalf("WriteManagedFile with nil manifest = %v, want nil", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != string(rendered) {
		t.Errorf("target bytes changed: %q, want %q", got, rendered)
	}
	assertAcceptedFileFrozen(t, path)
	assertNotExist(t, path+".scbake-new")
	assertNotExist(t, filepath.Join(tc.TargetPath, BaseShadowRelPath(task.OutputPath)))
}

// TestCreateTemplateTaskAdoptsEOLNormalizedIdenticalFile: the convergence check
// is EOL-insensitive, so a CRLF copy of the render is adopted and normalized.
func TestCreateTemplateTaskAdoptsEOLNormalizedIdenticalFile(t *testing.T) {
	tc, task, rendered := newIdenticalFileFixture(t, types.ConflictFail)
	crlf := []byte(strings.ReplaceAll(string(rendered), "\n", "\r\n"))
	path := writeTarget(t, tc, task, crlf)

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() on an EOL-normalized identical file = %v, want nil", err)
	}
	got, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != string(rendered) {
		t.Errorf("adopted bytes = %q, want the normalized render %q", got, rendered)
	}
	if got := tc.Manifest.ManagedFiles[task.OutputPath]; got != HashContent(rendered) {
		t.Errorf("managed_files[%q] = %q, want %q", task.OutputPath, got, HashContent(rendered))
	}
}

// TestCreateTemplateTaskGenuineModificationStillConflicts is the control: a
// genuinely edited file keeps the historic fail/artifact behavior.
//
//nolint:cyclop // fail and artifact control subtests
func TestCreateTemplateTaskGenuineModificationStillConflicts(t *testing.T) {
	t.Run("fail", func(t *testing.T) {
		tc, task, rendered := newIdenticalFileFixture(t, types.ConflictFail)
		local := append(append([]byte{}, rendered...), []byte("\n# local edit\n")...)
		path := writeTarget(t, tc, task, local)

		err := task.Execute(tc)
		if err == nil {
			t.Fatal("Execute() on a genuinely modified file = nil, want a conflict error")
		}
		if !strings.Contains(err.Error(), "manual modifications") {
			t.Errorf("error = %v, want a manual-modifications conflict", err)
		}
		got, readErr := os.ReadFile(path) //nolint:gosec // test-controlled temp path
		if readErr != nil {
			t.Fatalf("read target: %v", readErr)
		}
		if string(got) != string(local) {
			t.Errorf("modified file changed: %q", got)
		}
		assertAcceptedFileFrozen(t, path)
		assertNotExist(t, path+".scbake-new")
		if _, ok := tc.Manifest.ManagedFiles[task.OutputPath]; ok {
			t.Errorf("conflicting file was recorded as managed")
		}
	})

	t.Run("artifact", func(t *testing.T) {
		tc, task, rendered := newIdenticalFileFixture(t, types.ConflictArtifact)
		local := append(append([]byte{}, rendered...), []byte("\n# local edit\n")...)
		path := writeTarget(t, tc, task, local)

		if err := task.Execute(tc); err != nil {
			t.Fatalf("Execute() with artifact strategy = %v, want nil", err)
		}
		got, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
		if err != nil {
			t.Fatalf("read target: %v", err)
		}
		if string(got) != string(local) {
			t.Errorf("modified file changed: %q", got)
		}
		assertAcceptedFileFrozen(t, path)
		artifact, err := os.ReadFile(path + ".scbake-new") //nolint:gosec // test-controlled temp path
		if err != nil {
			t.Fatalf("artifact was not created: %v", err)
		}
		if string(artifact) != string(rendered) {
			t.Errorf("artifact = %q, want the render %q", artifact, rendered)
		}
		if _, ok := tc.Manifest.ManagedFiles[task.OutputPath]; ok {
			t.Errorf("conflicting file was recorded as managed")
		}
	})
}

// TestCreateTemplateTaskDryRunIdenticalFileNoConflict: the read-only preview
// accepts an identical file without writing, adopting, or creating an artifact.
func TestCreateTemplateTaskDryRunIdenticalFileNoConflict(t *testing.T) {
	tc, task, rendered := newIdenticalFileFixture(t, types.ConflictFail)
	path := writeTarget(t, tc, task, rendered)
	tc.DryRun = true
	tc.Tx = nil

	var validateErr error
	stderr := captureStderr(t, func() { validateErr = task.Validate(tc) })
	if validateErr != nil {
		t.Fatalf("Validate() on an identical file = %v, want nil", validateErr)
	}
	if strings.Contains(stderr, "manual modifications") || strings.Contains(stderr, "Conflict") {
		t.Errorf("dry run reported a conflict: %q", stderr)
	}

	var execErr error
	captureStderr(t, func() { execErr = task.Execute(tc) })
	if execErr != nil {
		t.Fatalf("Execute() in dry-run mode = %v, want nil", execErr)
	}
	assertAcceptedFileFrozen(t, path)
	assertNotExist(t, path+".scbake-new")
	assertNotExist(t, filepath.Join(tc.TargetPath, BaseShadowRelPath(task.OutputPath)))
	if _, ok := tc.Manifest.ManagedFiles[task.OutputPath]; ok {
		t.Errorf("dry run recorded the file as managed")
	}
}
