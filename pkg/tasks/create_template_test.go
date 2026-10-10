// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"testing"
)

//go:embed testdata/simple.tpl
var testTemplates embed.FS

func TestCreateTemplateTask(t *testing.T) {
	// Setup temp workspace
	tmpDir := t.TempDir()

	// Setup Task Context
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects: []types.Project{
			{Name: "MyProject"},
		},
	}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		Manifest:   manifest,
		DryRun:     false,
		Force:      false,
	}

	// Case 1: Render a valid template (Standard relative path)
	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "output.txt",
		Desc:         "Render simple template",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Template execution failed: %v", err)
	}

	// Verify Content
	// G304: Reading file from trusted temp directory in test scope
	//nolint:gosec
	content, err := os.ReadFile(filepath.Join(tmpDir, "output.txt"))
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}
	expected := "Hello MyProject from v1.0.0"
	if string(content) != expected {
		t.Errorf("Render mismatch. Got '%s', want '%s'", string(content), expected)
	}

	// Case 2: Idempotent re-run (should succeed — same hash, no drift)
	if err := task.Execute(tc); err != nil {
		t.Errorf("Idempotent re-run should succeed: %v", err)
	}

	// Case 3: Modify file externally, then re-run (drift detection)
	existingContent := append([]byte("MANUAL EDIT"), []byte("Hello MyProject from v1.0.0")...)
	//nolint:gosec
	if err := os.WriteFile(filepath.Join(tmpDir, "output.txt"), existingContent, 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}
	if err := task.Execute(tc); err == nil {
		t.Error("Expected error when overwriting drifted file without Force")
	}

	// Case 4: Force Overwrite (drifted file)
	tc.Force = true
	if err := task.Execute(tc); err != nil {
		t.Errorf("Force overwrite of drifted file failed: %v", err)
	}

	// Case 5: Path Traversal Attack (Security Check)
	// Try to write outside the target path
	attackTask := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "../escaped.txt", // Trying to go up
		Desc:         "Path traversal attempt",
	}

	// Reset Force
	tc.Force = false
	if err := attackTask.Execute(tc); err == nil {
		t.Error("Path traversal attack succeeded! It should have been blocked.")
	}
}

func TestCreateTemplateTask_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "DryRun"}},
	}
	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest:   manifest,
		DryRun:     true,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "dryrun.txt",
		Desc:         "Dry run test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() with dry-run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "dryrun.txt")); !os.IsNotExist(err) {
		t.Error("Dry-run should not create the file")
	}
}

func TestCreateTemplateTask_ConflictStrategyOverwrite(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "OverwriteTest"}},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         manifest,
		ConflictStrategy: "overwrite",
		DryRun:           false,
		Force:            false,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "overwrite.txt",
		Desc:         "Overwrite test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute failed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "overwrite.txt"), []byte("DRIFTED CONTENT"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Overwrite strategy should succeed: %v", err)
	}
}

func TestCreateTemplateTask_ConflictStrategyArtifact(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "ArtifactTest"}},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         manifest,
		ConflictStrategy: "artifact",
		DryRun:           false,
		Force:            false,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "artifact.txt",
		Desc:         "Artifact test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute failed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "artifact.txt"), []byte("DRIFTED"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Artifact strategy should succeed: %v", err)
	}

	artifactPath := filepath.Join(tmpDir, "artifact.txt.scbake-new")
	if _, err := os.Stat(artifactPath); os.IsNotExist(err) {
		t.Error("Artifact file was not created")
	}
}

func TestCreateTemplateTask_ConflictStrategyKeepLocal(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "KeepLocalTest"}},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         manifest,
		ConflictStrategy: "keep-local",
		DryRun:           false,
		Force:            false,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "keeplocal.txt",
		Desc:         "Keep local test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute failed: %v", err)
	}

	drifterContent := []byte("DRIFTED")
	if err := os.WriteFile(filepath.Join(tmpDir, "keeplocal.txt"), drifterContent, 0600); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Keep-local strategy should succeed without error: %v", err)
	}

	//nolint:gosec // Test temp directory
	content, _ := os.ReadFile(filepath.Join(tmpDir, "keeplocal.txt"))
	if string(content) != string(drifterContent) {
		t.Errorf("Keep-local should preserve local content. Got %q, want %q", string(content), string(drifterContent))
	}
}

func TestCreateTemplateTask_FileNeverManaged(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "UnmanagedTest"}},
		ManagedFiles: nil,
	}
	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest:   manifest,
		DryRun:     false,
		Force:      false,
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "preexisting.txt"), []byte("preexisting"), 0644); err != nil {
		t.Fatalf("Failed to create preexisting file: %v", err)
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "preexisting.txt",
		Desc:         "Unmanaged file test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err == nil {
		t.Error("Expected error for unmanaged preexisting file without force")
	}
}

func TestCreateTemplateTask_Transaction(t *testing.T) {
	// Setup with Transaction
	rootDir := t.TempDir()
	tx, _ := transaction.New(rootDir)

	// Provide a valid manifest so the template can render {{ (index .Projects 0).Name }}
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects: []types.Project{
			{Name: "TrackedProject"},
		},
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Manifest:   manifest,
		Tx:         tx,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "tracked_file.txt",
		Desc:         "Tracked File",
		TaskPrio:     100,
	}

	// Execute
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Task execution failed: %v", err)
	}

	// File should exist
	path := filepath.Join(rootDir, "tracked_file.txt")
	absPath, _ := filepath.Abs(path)

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		t.Fatal("File was not created")
	}

	// Rollback
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// File should be gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("File was not removed by rollback")
	}
}

func TestWriteManagedFile_RollbackRemovesIntermediateDirs(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Manifest:   &types.Manifest{ManagedFiles: make(map[string]string)},
		Tx:         tx,
	}

	// Write a file several directories deep. Those parent directories do not
	// exist yet, so WriteManagedFile must create AND track them.
	if err := WriteManagedFile(tc, "nested/deep/file.txt", []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteManagedFile() failed: %v", err)
	}

	filePath := filepath.Join(rootDir, "nested", "deep", "file.txt")
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("managed file was not created: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() failed: %v", err)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("managed file was not removed by rollback")
	}

	// Regression (SCB-055): implicitly-created parent directories must also be
	// removed so the filesystem returns to its pre-task state.
	if _, err := os.Stat(filepath.Join(rootDir, "nested")); !os.IsNotExist(err) {
		t.Errorf("intermediate directory %q was left behind by rollback", "nested")
	}
}

func TestWriteManagedFile_ArtifactDoesNotRecordNewHash(t *testing.T) {
	tmpDir := t.TempDir()
	rel := "config/app.yaml"

	// scbake generates the file once and records the hash of that original file.
	original := []byte("version: 1\n")
	originalHash := HashContent(original)
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		ManagedFiles: make(map[string]string),
	}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		Manifest:   manifest,
	}
	if err := WriteManagedFile(tc, rel, original, 0o644); err != nil {
		t.Fatalf("initial WriteManagedFile() failed: %v", err)
	}
	if got := manifest.ManagedFiles[rel]; got != originalHash {
		t.Fatalf("setup: ManagedFiles[%q] = %s, want %s", rel, got, originalHash)
	}

	// The user hand-edits the generated file, causing drift.
	absPath := filepath.Join(tmpDir, rel)
	drifted := []byte("version: 2 # hand edited\n")
	if err := os.WriteFile(absPath, drifted, 0o600); err != nil {
		t.Fatalf("failed to seed drift: %v", err)
	}

	// A later run wants to generate new content, but must not clobber the user's
	// edit: the artifact strategy writes it next to the original instead.
	newContent := []byte("version: 3\n")
	newHash := HashContent(newContent)
	tc.ConflictStrategy = "artifact"
	if err := WriteManagedFile(tc, rel, newContent, 0o644); err != nil {
		t.Fatalf("artifact WriteManagedFile() failed: %v", err)
	}

	// The artifact carries the freshly generated template.
	artifact, err := os.ReadFile(absPath + ".scbake-new") //nolint:gosec // trusted temp dir
	if err != nil {
		t.Fatalf("artifact file was not created: %v", err)
	}
	if string(artifact) != string(newContent) {
		t.Errorf("artifact content = %q, want %q", artifact, newContent)
	}

	// The original path is untouched.
	onDisk, err := os.ReadFile(absPath) //nolint:gosec // trusted temp dir
	if err != nil {
		t.Fatalf("original file unreadable: %v", err)
	}
	if string(onDisk) != string(drifted) {
		t.Errorf("original file was modified: got %q, want %q", onDisk, drifted)
	}

	// Regression (SCB-056): the ledger must keep the SHA-256 of the original
	// file we did NOT overwrite, not the hash of the new template that was only
	// written to the artifact.
	if got := manifest.ManagedFiles[rel]; got != originalHash {
		t.Errorf("ManagedFiles[%q] = %s, want original hash %s (new template hash is %s)", rel, got, originalHash, newHash)
	}
}

func TestWriteManagedFile_NormalWriteRecordsNewHash(t *testing.T) {
	tmpDir := t.TempDir()
	rel := "config/app.yaml"

	// Seed a drifted file plus the old recorded hash so reconciliation decides
	// to overwrite the original reconciled path.
	oldContent := []byte("version: 1\n")
	drifted := []byte("version: 2 # hand edited\n")
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		ManagedFiles: map[string]string{rel: HashContent(oldContent)},
	}
	absPath := filepath.Join(tmpDir, rel)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(absPath, drifted, 0o600); err != nil {
		t.Fatalf("failed to seed drift: %v", err)
	}

	newContent := []byte("version: 3\n")
	newHash := HashContent(newContent)
	tc := types.TaskContext{
		Ctx:              context.Background(),
		TargetPath:       tmpDir,
		Manifest:         manifest,
		ConflictStrategy: "overwrite",
	}
	if err := WriteManagedFile(tc, rel, newContent, 0o644); err != nil {
		t.Fatalf("WriteManagedFile() failed: %v", err)
	}

	// When the original path really is written, the new hash must be recorded.
	if got := manifest.ManagedFiles[rel]; got != newHash {
		t.Errorf("ManagedFiles[%q] = %s, want new hash %s", rel, got, newHash)
	}
	onDisk, err := os.ReadFile(absPath) //nolint:gosec // trusted temp dir
	if err != nil {
		t.Fatalf("original file unreadable: %v", err)
	}
	if string(onDisk) != string(newContent) {
		t.Errorf("original file = %q, want overwritten content %q", onDisk, newContent)
	}
}

func TestCreateTemplateTask_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "TraversalTest"}},
	}

	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest:   manifest,
	}

	traversalOutputs := []string{
		"../../etc/passwd",
		"../secret.txt",
		"sub/../../secret.txt",
	}

	for _, output := range traversalOutputs {
		t.Run(output, func(t *testing.T) {
			task := &CreateTemplateTask{
				TemplateFS:   testTemplates,
				TemplatePath: "testdata/simple.tpl",
				OutputPath:   output,
				Desc:         "Path traversal output test",
				TaskPrio:     100,
			}

			if err := task.Execute(tc); err == nil {
				t.Errorf("Expected error for output path traversal payload %q, got nil", output)
			}
		})
	}
}

// TestWriteManagedFile_RollbackRemovesMissingTargetSubdir is a regression test
// for SCB-055: the ancestor walk must be bounded by the transaction root, not by
// tc.TargetPath, otherwise a not-yet-existing target directory (monorepo
// subproject apply) is created but never tracked and survives rollback.
func TestWriteManagedFile_RollbackRemovesMissingTargetSubdir(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}

	// The subproject target does not exist yet; the transaction root does.
	target := filepath.Join(root, "services", "api")
	tc := types.TaskContext{TargetPath: target, Manifest: &types.Manifest{}, Tx: tx}

	if err := WriteManagedFile(tc, filepath.Join("pkg", "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteManagedFile() failed: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "services")); !os.IsNotExist(err) {
		t.Errorf("rollback left the implicitly-created target tree %q behind", filepath.Join(root, "services"))
	}
}

// TestWriteManagedFile_ManagedFilesScopedByProject is a regression test for
// SCB-057: two monorepo subprojects that generate the same relative filename
// must get distinct managed_files entries, so re-applying one does not report
// the other subproject's content as drift.
func TestWriteManagedFile_ManagedFilesScopedByProject(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}

	manifest := &types.Manifest{}
	subA := types.TaskContext{TargetPath: filepath.Join(root, "services", "a"), Manifest: manifest, Tx: tx}
	subB := types.TaskContext{TargetPath: filepath.Join(root, "services", "b"), Manifest: manifest, Tx: tx}

	contentA := []byte("license for alice\n")
	contentB := []byte("license for bob\n")
	if err := WriteManagedFile(subA, "LICENSE", contentA, 0o600); err != nil {
		t.Fatalf("writing subproject A failed: %v", err)
	}
	if err := WriteManagedFile(subB, "LICENSE", contentB, 0o600); err != nil {
		t.Fatalf("writing subproject B failed: %v", err)
	}

	keyA := "services/a/LICENSE"
	keyB := "services/b/LICENSE"
	if got := manifest.ManagedFiles[keyA]; got != HashContent(contentA) {
		t.Errorf("ManagedFiles[%q] = %q, want hash of A's content", keyA, got)
	}
	if got := manifest.ManagedFiles[keyB]; got != HashContent(contentB) {
		t.Errorf("ManagedFiles[%q] = %q, want hash of B's content", keyB, got)
	}
	if len(manifest.ManagedFiles) != 2 {
		t.Errorf("expected 2 distinct ledger entries, got %d: %v", len(manifest.ManagedFiles), manifest.ManagedFiles)
	}

	// Re-applying subproject A with NEW content must not report drift. Before
	// the fix both subprojects shared the "LICENSE" key, so B's hash overwrote
	// A's and this re-apply failed with a false "manual modifications" error.
	contentA2 := []byte("license for alice v2\n")
	if err := WriteManagedFile(subA, "LICENSE", contentA2, 0o600); err != nil {
		t.Fatalf("re-applying subproject A with new content reported drift: %v", err)
	}
	if got := manifest.ManagedFiles[keyA]; got != HashContent(contentA2) {
		t.Errorf("ManagedFiles[%q] = %q, want the updated hash", keyA, got)
	}
}

// TestWriteManagedFile_StaleLegacyKeyNotTrusted verifies that a legacy
// project-relative key which does not match the file's on-disk content is not
// used as a drift baseline. Otherwise another subproject's stale entry could
// mask real drift or cause a silent overwrite.
func TestWriteManagedFile_StaleLegacyKeyNotTrusted(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}

	target := filepath.Join(root, "services", "a")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	onDisk := []byte("alice license\n")
	if err := os.WriteFile(filepath.Join(target, "LICENSE"), onDisk, 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// The legacy flat key holds a DIFFERENT project's hash. The new content
	// equals what is already on disk, which is the case where a blindly-trusted
	// legacy key would take the idempotency path and silently accept the file.
	manifest := &types.Manifest{ManagedFiles: map[string]string{"LICENSE": HashContent([]byte("bob license\n"))}}
	tc := types.TaskContext{TargetPath: target, Manifest: manifest, Tx: tx}

	if err := WriteManagedFile(tc, "LICENSE", onDisk, 0o600); err == nil {
		t.Fatal("expected a conflict: a mismatched legacy key must not be trusted as a baseline")
	}
}

// TestWriteManagedFile_LegacyFlatKeyStillRecognized verifies backward
// compatibility: manifests written before keys were scoped stored the
// project-relative path, which must still be honored rather than treated as an
// unmanaged (drifting) file.
func TestWriteManagedFile_LegacyFlatKeyStillRecognized(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}

	target := filepath.Join(root, "services", "a")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	content := []byte("license\n")
	if err := os.WriteFile(filepath.Join(target, "LICENSE"), content, 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	manifest := &types.Manifest{ManagedFiles: map[string]string{"LICENSE": HashContent(content)}}
	tc := types.TaskContext{TargetPath: target, Manifest: manifest, Tx: tx}
	if err := WriteManagedFile(tc, "LICENSE", content, 0o600); err != nil {
		t.Fatalf("legacy flat key not honored (false drift): %v", err)
	}
}
