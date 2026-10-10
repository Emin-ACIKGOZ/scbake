package tasks

import (
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
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

// TestAppendFileTask_SubstringIsNotIdempotent guards against the substring-based
// idempotency bug: content that only appears as a substring of unrelated text
// must still be appended, and a second run must be a no-op.
func TestAppendFileTask_SubstringIsNotIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	// The task content ("content") appears inside "some-extra-content" but never
	// as its own standalone block.
	initial := "some-extra-content"
	//nolint:gosec // Test file
	if err := os.WriteFile(filepath.Join(tmpDir, "substring.txt"), []byte(initial), 0644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	task := &AppendFileTask{
		FilePath: "substring.txt",
		Content:  "content",
		Desc:     "substring regression",
		TaskPrio: 100,
	}
	tc := types.TaskContext{TargetPath: tmpDir, DryRun: false}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	want := "some-extra-content\ncontent"
	got, err := fileutil.SafeReadFile(tmpDir, "substring.txt")
	if err != nil {
		t.Fatalf("SafeReadFile() failed: %v", err)
	}
	if string(got) != want {
		t.Fatalf("content was silently skipped. Want %q, got %q", want, string(got))
	}

	// Second execution must not append a duplicate.
	if err := task.Execute(tc); err != nil {
		t.Fatalf("second Execute() failed: %v", err)
	}
	got2, err := fileutil.SafeReadFile(tmpDir, "substring.txt")
	if err != nil {
		t.Fatalf("SafeReadFile() after second run failed: %v", err)
	}
	if string(got2) != want {
		t.Errorf("second execution was not idempotent. Want %q, got %q", want, string(got2))
	}
}

// TestAppendFileTask_StandaloneBlockIdempotent verifies that a block already
// present as its own line is not appended again.
func TestAppendFileTask_StandaloneBlockIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	initial := "line 1\ncontent\nline 3"
	//nolint:gosec // Test file
	if err := os.WriteFile(filepath.Join(tmpDir, "block.txt"), []byte(initial), 0644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	task := &AppendFileTask{
		FilePath: "block.txt",
		Content:  "content",
		Desc:     "standalone block",
		TaskPrio: 100,
	}
	tc := types.TaskContext{TargetPath: tmpDir, DryRun: false}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	got, err := fileutil.SafeReadFile(tmpDir, "block.txt")
	if err != nil {
		t.Fatalf("SafeReadFile() failed: %v", err)
	}
	if string(got) != initial {
		t.Errorf("standalone block was duplicated. Want %q, got %q", initial, string(got))
	}
}

// TestAppendFileTask_Multiline covers multi-line content that is not present as
// a standalone block: either only its first line matches, or it matches as a
// substring that spans unrelated line boundaries.
func TestAppendFileTask_Multiline(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		content string
		want    string
	}{
		{
			// Only the first line of the multi-line content already exists.
			name:    "prefix line only",
			initial: "alpha",
			content: "alpha\nbeta",
			want:    "alpha\nalpha\nbeta",
		},
		{
			// The content appears as a raw substring across unrelated lines,
			// but is not aligned to its own block.
			name:    "substring spanning line boundary",
			initial: "xalpha\nbeta y",
			content: "alpha\nbeta",
			want:    "xalpha\nbeta y\nalpha\nbeta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			//nolint:gosec // Test file
			if err := os.WriteFile(filepath.Join(tmpDir, "multiline.txt"), []byte(tt.initial), 0644); err != nil {
				t.Fatalf("seeding file: %v", err)
			}

			task := &AppendFileTask{
				FilePath: "multiline.txt",
				Content:  tt.content,
				Desc:     "multiline",
				TaskPrio: 100,
			}
			tc := types.TaskContext{TargetPath: tmpDir, DryRun: false}

			if err := task.Execute(tc); err != nil {
				t.Fatalf("Execute() failed: %v", err)
			}

			got, err := fileutil.SafeReadFile(tmpDir, "multiline.txt")
			if err != nil {
				t.Fatalf("SafeReadFile() failed: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("multi-line content was incorrectly skipped. Want %q, got %q", tt.want, string(got))
			}

			// Second execution must be a no-op.
			if err := task.Execute(tc); err != nil {
				t.Fatalf("second Execute() failed: %v", err)
			}
			got2, err := fileutil.SafeReadFile(tmpDir, "multiline.txt")
			if err != nil {
				t.Fatalf("SafeReadFile() after second run failed: %v", err)
			}
			if string(got2) != tt.want {
				t.Errorf("second execution was not idempotent. Want %q, got %q", tt.want, string(got2))
			}
		})
	}
}

func TestAppendFileTask_PathTraversalRejected(t *testing.T) {
	tmpDir := t.TempDir()
	traversalPayloads := []string{
		"../secret.txt",
		"../../etc/passwd",
		"/etc/passwd",
		"sub/../../secret.txt",
	}

	for _, payload := range traversalPayloads {
		t.Run(payload, func(t *testing.T) {
			task := &AppendFileTask{
				FilePath: payload,
				Content:  "unauthorized content",
				Desc:     "Path traversal append safety test",
				TaskPrio: 100,
			}

			tc := types.TaskContext{
				TargetPath: tmpDir,
				DryRun:     true, // DryRun ensures zero filesystem modification
			}

			err := task.Execute(tc)
			if err == nil {
				t.Errorf("Path traversal contract violation: AppendFileTask unexpectedly accepted traversal payload %q", payload)
			}
		})
	}
}
