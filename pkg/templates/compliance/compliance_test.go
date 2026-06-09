// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package compliance

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"testing"
)

func TestComplianceHandler_GetTasks(t *testing.T) {
	h := &Handler{}
	tasks, err := h.GetTasks(".", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	// SECURITY.md, dependabot.yml, LICENSE, CODEOWNERS
	expectedCount := 4
	if len(tasks) != expectedCount {
		t.Errorf("Expected %d tasks, got %d", expectedCount, len(tasks))
	}
}

func TestLicenseTask_Execute(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		metadata map[string]string
		wantErr  bool
	}{
		{
			"Valid Metadata",
			map[string]string{"license": "MIT", "copyright_holder": "Test Holder"},
			false,
		},
		{
			"Missing Metadata",
			nil,
			true,
		},
		{
			"Missing License",
			map[string]string{"copyright_holder": "Test Holder"},
			true,
		},
		{
			"Unsupported License",
			map[string]string{"license": "INVALID", "copyright_holder": "Test Holder"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &types.Manifest{Metadata: tt.metadata}
			tc := types.TaskContext{
				Ctx:        context.Background(),
				TargetPath: tmpDir,
				Manifest:   m,
			}

			task := &LicenseTask{TaskPrio: 100}
			err := task.Execute(tc)

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				licensePath := filepath.Join(tmpDir, "LICENSE")
				if _, err := os.Stat(licensePath); err != nil {
					t.Errorf("LICENSE file not created")
				}
			}
		})
	}
}

func TestLicenseTask_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{Metadata: map[string]string{"license": "MIT", "copyright_holder": "DryRun Holder"}}
	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest:   m,
		DryRun:     true,
	}
	task := &LicenseTask{TaskPrio: 100}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() with dry-run failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "LICENSE")); !os.IsNotExist(err) {
		t.Error("Dry-run should not create LICENSE file")
	}
}

func TestLicenseTask_ConflictStrategyFail(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata:     map[string]string{"license": "MIT", "copyright_holder": "Fail Holder"},
		ManagedFiles: map[string]string{"LICENSE": "originalhash"},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         m,
		ConflictStrategy: "fail",
	}
	task := &LicenseTask{TaskPrio: 100}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute should succeed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "LICENSE"), []byte("DRIFTED CONTENT"), 0644); err != nil {
		t.Fatalf("Failed to drift file: %v", err)
	}

	if err := task.Execute(tc); err == nil {
		t.Error("Expected drift error with 'fail' strategy")
	}
}

func TestLicenseTask_ConflictStrategyOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata:     map[string]string{"license": "MIT", "copyright_holder": "Overwrite Holder"},
		ManagedFiles: map[string]string{"LICENSE": "originalhash"},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         m,
		ConflictStrategy: "overwrite",
	}
	task := &LicenseTask{TaskPrio: 100}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute should succeed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "LICENSE"), []byte("DRIFTED"), 0644); err != nil {
		t.Fatalf("Failed to drift file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Overwrite strategy should succeed: %v", err)
	}
}

func TestLicenseTask_ConflictStrategyArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata:     map[string]string{"license": "MIT", "copyright_holder": "Artifact Holder"},
		ManagedFiles: map[string]string{"LICENSE": "originalhash"},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         m,
		ConflictStrategy: "artifact",
	}
	task := &LicenseTask{TaskPrio: 100}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute should succeed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "LICENSE"), []byte("DRIFTED"), 0644); err != nil {
		t.Fatalf("Failed to drift file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Artifact strategy should succeed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "LICENSE.scbake-new")); os.IsNotExist(err) {
		t.Error("Artifact file was not created")
	}
}

func TestLicenseTask_ConflictStrategyKeepLocal(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata:     map[string]string{"license": "MIT", "copyright_holder": "KeepLocal Holder"},
		ManagedFiles: map[string]string{"LICENSE": "originalhash"},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         m,
		ConflictStrategy: "keep-local",
	}
	task := &LicenseTask{TaskPrio: 100}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute should succeed: %v", err)
	}

	drifterContent := []byte("DRIFTED KEEPLOCAL")
	if err := os.WriteFile(filepath.Join(tmpDir, "LICENSE"), drifterContent, 0600); err != nil {
		t.Fatalf("Failed to drift file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Keep-local should succeed: %v", err)
	}

	//nolint:gosec // Test temp directory
	content, _ := os.ReadFile(filepath.Join(tmpDir, "LICENSE"))
	if string(content) != string(drifterContent) {
		t.Errorf("Keep-local should preserve local content. Got %q, want %q", string(content), string(drifterContent))
	}
}

func TestLicenseTask_ManagedFilesTracking(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata: map[string]string{"license": "MIT", "copyright_holder": "Tracking Holder"},
	}
	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest:   m,
	}
	task := &LicenseTask{TaskPrio: 100}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if _, ok := m.ManagedFiles["LICENSE"]; !ok {
		t.Error("ManagedFiles should track LICENSE after execution")
	}
}

func TestLicenseTask_Metadata(t *testing.T) {
	task := &LicenseTask{TaskPrio: 150}
	if task.Description() != "Generate LICENSE file" {
		t.Errorf("Description() mismatch")
	}
	if task.Priority() != 150 {
		t.Errorf("Priority() mismatch")
	}
}
