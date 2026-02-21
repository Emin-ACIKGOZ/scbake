package manifest

import (
	"os"
	"scbake/internal/types"
	"testing"
)

func TestLoadAndSave(t *testing.T) {
	// 1. Setup: Create a temporary directory for this test
	// t.TempDir automatically cleans up after the test finishes.
	tmpDir := t.TempDir()

	// Store current directory to restore it later
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current wd: %v", err)
	}

	// Switch to the temp dir so Load() looks for scbake.toml THERE, not in your actual repo
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp: %v", err)
	}
	// Defer the restoration of the original directory
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	// 2. Test Load on a fresh directory (Should return empty default)
	// This verifies handling of os.IsNotExist
	m, err := Load()
	if err != nil {
		t.Fatalf("Load() failed on empty dir: %v", err)
	}
	if len(m.Projects) != 0 {
		t.Error("expected empty projects list on new load")
	}

	// 3. Test Save: Add data and write to disk
	m.Projects = append(m.Projects, types.Project{
		Name:     "test-project",
		Path:     "./test",
		Language: "go",
	})

	if err := Save(m); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// 4. Test Reload: Read it back and verify data persistence
	m2, err := Load()
	if err != nil {
		t.Fatalf("Load() failed on second attempt: %v", err)
	}

	if len(m2.Projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(m2.Projects))
	}
	if m2.Projects[0].Name != "test-project" {
		t.Errorf("expected project name 'test-project', got %s", m2.Projects[0].Name)
	}
}
