package tasks

import (
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
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

func TestAppendFileTask_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "dryrun.txt")

	task := &AppendFileTask{
		FilePath: "dryrun.txt",
		Content:  "should not appear",
		Desc:     "Dry run test",
		TaskPrio: 100,
	}

	tc := types.TaskContext{
		TargetPath: tmpDir,
		DryRun:     true,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() with dry-run failed: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Dry-run should not create the file")
	}
}

func TestAppendFileTask_DryRunExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "dryrun-existing.txt")
	//nolint:gosec // Test file
	_ = os.WriteFile(path, []byte("initial"), 0644)

	task := &AppendFileTask{
		FilePath: "dryrun-existing.txt",
		Content:  "should not appear",
		Desc:     "Dry run existing",
		TaskPrio: 100,
	}

	tc := types.TaskContext{
		TargetPath: tmpDir,
		DryRun:     true,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() with dry-run failed: %v", err)
	}

	//nolint:gosec // Test file
	got, _ := os.ReadFile(path)
	if string(got) != "initial" {
		t.Errorf("Dry-run should not modify existing file. Got %q", string(got))
	}
}

func TestAppendFileTask_EmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty.txt")

	task := &AppendFileTask{
		FilePath: "empty.txt",
		Content:  "",
		Desc:     "Empty content",
		TaskPrio: 100,
	}

	tc := types.TaskContext{
		TargetPath: tmpDir,
		DryRun:     false,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() with empty content failed: %v", err)
	}

	//nolint:gosec // Test file
	got, _ := os.ReadFile(path)
	if string(got) != "" {
		t.Errorf("Expected empty file, got %q", string(got))
	}
}

func TestAppendFileTask_MultipleAppends(t *testing.T) {
	tmpDir := t.TempDir()

	task1 := &AppendFileTask{FilePath: "multi.txt", Content: "first", Desc: "first", TaskPrio: 100}
	task2 := &AppendFileTask{FilePath: "multi.txt", Content: "second", Desc: "second", TaskPrio: 101}

	tc := types.TaskContext{TargetPath: tmpDir, DryRun: false}

	if err := task1.Execute(tc); err != nil {
		t.Fatalf("First append failed: %v", err)
	}
	if err := task2.Execute(tc); err != nil {
		t.Fatalf("Second append failed: %v", err)
	}

	path := filepath.Join(tmpDir, "multi.txt")
	//nolint:gosec // Test file
	got, _ := os.ReadFile(path)
	if string(got) != "first\nsecond" {
		t.Errorf("Multiple appends produced wrong content %q", string(got))
	}
}

func TestAppendFileTask_TransactionRollback(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}

	task := &AppendFileTask{
		FilePath: "tracked.txt",
		Content:  "tracked content",
		Desc:     "Transaction test",
		TaskPrio: 100,
	}

	tc := types.TaskContext{
		TargetPath: rootDir,
		Tx:         tx,
		DryRun:     false,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	path := filepath.Join(rootDir, "tracked.txt")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("File should exist before rollback")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("File should be removed after rollback")
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
