// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"os"
	"path/filepath"
	"scbake/internal/types"
	"testing"
)

func TestAppendFileTask_Execute(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		filePath string
		initial  string
		append   string
		want     string
	}{
		{
			"New File",
			"new.txt",
			"",
			"hello world",
			"hello world",
		},
		{
			"Existing File - Append",
			"existing.txt",
			"line 1",
			"line 2",
			"line 1\nline 2",
		},
		{
			"Existing File - Idempotent",
			"idempotent.txt",
			"already there",
			"already there",
			"already there",
		},
		{
			"Existing File - Ends with newline",
			"newline.txt",
			"line 1\n",
			"line 2",
			"line 1\nline 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.filePath)
			if tt.initial != "" {
				//nolint:gosec // Testing with specific permissions
				_ = os.WriteFile(path, []byte(tt.initial), 0644)
			}

			task := &AppendFileTask{
				FilePath: tt.filePath,
				Content:  tt.append,
				Desc:     "Append test",
				TaskPrio: 100,
			}

			tc := types.TaskContext{
				TargetPath: tmpDir,
				DryRun:     false,
			}

			if err := task.Execute(tc); err != nil {
				t.Fatalf("Execute() failed: %v", err)
			}

			//nolint:gosec // Testing with specific permissions
			got, _ := os.ReadFile(path)
			if string(got) != tt.want {
				t.Errorf("Content mismatch. Want %q, Got %q", tt.want, string(got))
			}
		})
	}
}

func TestAppendFileTask_Metadata(t *testing.T) {
	task := &AppendFileTask{
		FilePath: "test.txt",
		Content:  "content",
		Desc:     "test desc",
		TaskPrio: 150,
	}

	if task.Description() != "test desc" {
		t.Errorf("Description() mismatch")
	}
	if task.Priority() != 150 {
		t.Errorf("Priority() mismatch")
	}
}
