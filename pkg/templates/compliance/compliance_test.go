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
	tasks, err := h.GetTasks(".")
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

func TestLicenseTask_Metadata(t *testing.T) {
	task := &LicenseTask{TaskPrio: 150}
	if task.Description() != "Generate LICENSE file" {
		t.Errorf("Description() mismatch")
	}
	if task.Priority() != 150 {
		t.Errorf("Priority() mismatch")
	}
}
