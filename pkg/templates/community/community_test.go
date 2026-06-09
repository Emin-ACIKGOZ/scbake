package community

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"testing"
)

func TestCommunityHandler_GetTasks(t *testing.T) {
	h := &Handler{}
	tasks, err := h.GetTasks(".")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	expectedFiles := map[string]bool{
		"CONTRIBUTING.md":   true,
		"CODE_OF_CONDUCT.md": true,
		"SUPPORT.md":         true,
		"GOVERNANCE.md":      true,
	}

	if len(tasks) != len(expectedFiles) {
		t.Errorf("Expected %d tasks, got %d", len(expectedFiles), len(tasks))
	}

	for _, task := range tasks {
		desc := task.Description()
		found := false
		for file := range expectedFiles {
			if desc == "Create "+file {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Unexpected task: %s", desc)
		}
	}
}

func TestCommunityHandler_ExecuteCreateContributing(t *testing.T) {
	h := &Handler{}
	plan, err := h.GetTasks(".")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	if len(plan) == 0 {
		t.Fatal("Expected at least one task")
	}

	tmpDir := t.TempDir()
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects: []types.Project{
			{Name: "TestProject"},
		},
	}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		Manifest:   manifest,
		DryRun:     false,
		Force:      false,
	}

	for _, task := range plan {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Execute(%s) failed: %v", task.Description(), err)
		}
	}

	for _, file := range []string{"CONTRIBUTING.md", "CODE_OF_CONDUCT.md", "SUPPORT.md", "GOVERNANCE.md"} {
		path := filepath.Join(tmpDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("%s was not created", file)
		}
	}
}

func TestCommunityHandler_ExecuteDryRun(t *testing.T) {
	h := &Handler{}
	plan, err := h.GetTasks(".")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	if len(plan) == 0 {
		t.Fatal("Expected at least one task")
	}

	tmpDir := t.TempDir()
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects: []types.Project{
			{Name: "TestProject"},
		},
	}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		Manifest:   manifest,
		DryRun:     true,
		Force:      false,
	}

	for _, task := range plan {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Execute(%s) in dry-run failed: %v", task.Description(), err)
		}
	}

	for _, file := range []string{"CONTRIBUTING.md", "CODE_OF_CONDUCT.md", "SUPPORT.md", "GOVERNANCE.md"} {
		path := filepath.Join(tmpDir, file)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s was created in dry-run mode", file)
		}
	}
}
