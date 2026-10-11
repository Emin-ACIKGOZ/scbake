// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/ui"
	"scbake/internal/util/fileutil"
	"testing"
)

// MockTask implements types.Task for testing core logic.
type MockTask struct {
	Name         string
	Prio         int
	PathToCreate string // If set, creates this file
	ShouldFail   bool   // If set, returns error
}

func (m *MockTask) Description() string { return m.Name }
func (m *MockTask) Priority() int       { return m.Prio }

func (m *MockTask) Execute(tc types.TaskContext) error {
	if m.PathToCreate != "" {
		// Verify tracking works from within the core engine
		if tc.Tx != nil {
			if err := tc.Tx.Track(m.PathToCreate); err != nil {
				return err
			}
		}
		// Create file
		// G306: Use PrivateFilePerms for secure test file creation
		if err := os.WriteFile(m.PathToCreate, []byte("test data"), fileutil.PrivateFilePerms); err != nil {
			return err
		}

		// Record managed file state (simulating real task behavior)
		if tc.Manifest.ManagedFiles == nil {
			tc.Manifest.ManagedFiles = make(map[string]string)
		}
		tc.Manifest.ManagedFiles[m.PathToCreate] = "testhash"
	}

	if m.ShouldFail {
		return errors.New("mock failure")
	}
	return nil
}

// TestExecuteAndFinalize_Rollback verifies that if a task fails,
// previous changes are rolled back.
func TestExecuteAndFinalize_Rollback(t *testing.T) {
	// Setup
	rootDir := t.TempDir()

	// Create initial manifest
	manifestPath := filepath.Join(rootDir, fileutil.ManifestFileName)
	// G306: Use PrivateFilePerms for secure test file creation
	if err := os.WriteFile(manifestPath, []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	// Init transaction manually (simulating RunApply)
	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}
	// We do NOT defer tx.Rollback() here because we want to test its effect explicitly
	// after the failure to ensure deterministic testing.

	// Define paths
	file1 := filepath.Join(rootDir, "step1.txt")

	// Build Plan: Task 1 succeeds (creates file), Task 2 fails.
	plan := &types.Plan{
		Tasks: []types.Task{
			&MockTask{Name: "Step 1", PathToCreate: file1},
			&MockTask{Name: "Step 2", ShouldFail: true},
		},
	}

	// Setup Context
	// Correctly capture rootPath from Load
	m, rootPath, err := manifest.Load(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	changes := &manifestChanges{}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Manifest:   m,
		Tx:         tx,
	}

	// Run logic
	// executeAndFinalize calls Step() 3 times (Execute, Update Manifest, Commit).
	// We use 3 as the denominator to ensure the UI doesn't drift.
	reporter := ui.NewPlainReporter(3, false)
	// Execute should fail
	err = executeAndFinalize(reporter, plan, tc, m, changes, rootPath, tx)

	// Assert Failure
	if err == nil {
		t.Fatal("Expected execution to fail, but it succeeded")
	}

	// Assert Rollback
	// executeAndFinalize returned error, so RunApply's defer would trigger Rollback.
	// We manually trigger rollback here to simulate that behavior and verify result.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// Check if file1 (created by Step 1) is gone.
	if _, err := os.Stat(file1); !os.IsNotExist(err) {
		t.Errorf("Rollback failed. File %s should have been deleted.", file1)
	}
}

// TestExecuteAndFinalize_Success verifies that a successful run commits changes
// and cleans up the temp directory.
// TestExecuteAndFinalize_ManagedFilesRecords verifies that after a successful
// execution, ManagedFiles are recorded in the manifest for reconciliation.
func TestExecuteAndFinalize_ManagedFilesRecords(t *testing.T) {
	rootDir := t.TempDir()

	manifestPath := filepath.Join(rootDir, fileutil.ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	file1 := filepath.Join(rootDir, "managed.txt")

	plan := &types.Plan{
		Tasks: []types.Task{
			&MockTask{Name: "Managed File", PathToCreate: file1},
		},
	}

	m, rootPath, err := manifest.Load(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	changes := &manifestChanges{}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Manifest:   m,
		Tx:         tx,
	}

	reporter := ui.NewPlainReporter(3, false)
	if err := executeAndFinalize(reporter, plan, tc, m, changes, rootPath, tx); err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	if m.ManagedFiles == nil {
		t.Fatal("ManagedFiles should be initialized after successful execution")
	}
	if _, ok := m.ManagedFiles[file1]; !ok {
		t.Error("file1 should be tracked in ManagedFiles")
	}
}

// TestExecuteAndFinalize_EmptyPlan verifies that a plan with no tasks completes
// without error and records no ManagedFiles.
func TestExecuteAndFinalize_EmptyPlan(t *testing.T) {
	rootDir := t.TempDir()

	manifestPath := filepath.Join(rootDir, fileutil.ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	plan := &types.Plan{
		Tasks: []types.Task{},
	}

	m, rootPath, err := manifest.Load(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	changes := &manifestChanges{}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Manifest:   m,
		Tx:         tx,
	}

	reporter := ui.NewPlainReporter(0, false)
	if err := executeAndFinalize(reporter, plan, tc, m, changes, rootPath, tx); err != nil {
		t.Fatalf("Empty plan execution should succeed: %v", err)
	}
}

func TestExecuteAndFinalize_Success(t *testing.T) {
	rootDir := t.TempDir()

	manifestPath := filepath.Join(rootDir, fileutil.ManifestFileName)
	// G306: Use PrivateFilePerms for secure test file creation
	if err := os.WriteFile(manifestPath, []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	// Explicitly check initialization error
	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}

	// In success case, Commit() invalidates the transaction, so defer Rollback is harmless.
	defer func() { _ = tx.Rollback() }()

	file1 := filepath.Join(rootDir, "success.txt")

	plan := &types.Plan{
		Tasks: []types.Task{
			&MockTask{Name: "Success", PathToCreate: file1},
		},
	}

	// Correctly capture rootPath from Load
	m, rootPath, err := manifest.Load(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	// Propose a change to manifest to verify it saves
	changes := &manifestChanges{
		Projects: []types.Project{{Name: "NewProj", Path: "."}},
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Manifest:   m,
		Tx:         tx,
	}

	// executeAndFinalize calls Step() exactly 3 times.
	reporter := ui.NewPlainReporter(3, false)
	if err := executeAndFinalize(reporter, plan, tc, m, changes, rootPath, tx); err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	// Assert Persistence
	if _, err := os.Stat(file1); os.IsNotExist(err) {
		t.Error("File was not created")
	}

	// Assert Manifest Update
	updatedM, _, _ := manifest.Load(rootDir)
	if len(updatedM.Projects) != 1 {
		t.Error("Manifest was not updated")
	}

	// Assert Cleanup (Temp dir gone)
	tmpDir := filepath.Join(rootDir, fileutil.InternalDir, fileutil.TmpDir)

	// We read the directory.
	// 1. If entries exist > 0: FAIL (cleanup didn't happen)
	// 2. If err is nil (dir exists) but empty: PASS (transaction committed)
	// 3. If err is IsNotExist: PASS (parent folder removed, highly clean)
	entries, err := os.ReadDir(tmpDir)
	if err == nil && len(entries) > 0 {
		t.Errorf("Temp directory %s is not empty after commit: %v", tmpDir, entries)
	}
	// Note: We don't fail on IsNotExist, as that is a valid clean state.
}

// TestValidateProjectPath is a regression guard for SCB-006/SCB-007/SCB-008:
// project paths are interpolated verbatim into generated shell scripts,
// Makefile recipes, and YAML, so anything that could escape those scalar
// contexts must be rejected at plan time.
func TestValidateProjectPath(t *testing.T) {
	valid := []string{
		"backend",
		"services/api",
		"services/web-app",
		".",
		"./services/api",
		"a_b-c.d/e",
	}
	for _, p := range valid {
		if err := validateProjectPath(p); err != nil {
			t.Errorf("validateProjectPath(%q) = %v, want nil", p, err)
		}
	}

	invalid := []string{
		"",
		"/etc/passwd",
		"/",
		"../outside",
		"services/../../etc",
		"a/../b",
		`services\api`,
		"services/x; touch pwned",
		"services/$(touch pwned)",
		"services/x`touch pwned`",
		"services/x && touch pwned",
		"services/x|touch pwned",
		"services/x\ntouch pwned",
		"services/x\ttouch pwned",
		"services/my project",
		"C:/windows",
		"services/x\"y",
		"services/x'y",
	}
	for _, p := range invalid {
		if err := validateProjectPath(p); err == nil {
			t.Errorf("validateProjectPath(%q) = nil, want error", p)
		}
	}
}

// TestValidateProjectName is a regression guard for SCB-006: the project name
// is echoed into shell `run:` blocks and Makefile recipes.
func TestValidateProjectName(t *testing.T) {
	valid := []string{"backend", "web-app", "my_app.v2", "NewProj", "a1"}
	for _, n := range valid {
		if err := validateProjectName(n); err != nil {
			t.Errorf("validateProjectName(%q) = %v, want nil", n, err)
		}
	}

	invalid := []string{
		"",
		"evil; touch pwned",
		"evil && touch pwned",
		"evil$(touch pwned)",
		"evil`touch pwned`",
		"evil\ntouch pwned",
		"evil\ttouch pwned",
		"evil name",
		`evil": {a: b}`,
		"evil/path",
		"evil\\path",
	}
	for _, n := range invalid {
		if err := validateProjectName(n); err == nil {
			t.Errorf("validateProjectName(%q) = nil, want error", n)
		}
	}
}

// TestValidateProjectLanguage guards the language field, which is embedded in
// Makefile variable names (MAKE_BUILD_COMMAND_<lang>).
func TestValidateProjectLanguage(t *testing.T) {
	for _, l := range []string{"", "go", "svelte", "spring", "rust"} {
		if err := validateProjectLanguage(l); err != nil {
			t.Errorf("validateProjectLanguage(%q) = %v, want nil", l, err)
		}
	}
	for _, l := range []string{"go; touch pwned", "go) ; x", "go\nx", "go x", "go/x"} {
		if err := validateProjectLanguage(l); err == nil {
			t.Errorf("validateProjectLanguage(%q) = nil, want error", l)
		}
	}
}

// TestValidateTestCommand is a regression guard for SCB-007: test_command is
// intentionally executed by the Makefile, but control characters could break
// out of the recipe.
func TestValidateTestCommand(t *testing.T) {
	valid := []string{"go test ./...", "npm test", "make test && echo done", "pytest -q"}
	for _, c := range valid {
		if err := validateTestCommand(c); err != nil {
			t.Errorf("validateTestCommand(%q) = %v, want nil", c, err)
		}
	}
	invalid := []string{"go test ./...\nrm -rf /", "go\ttest", "go test\r\n", "go test\x00"}
	for _, c := range invalid {
		if err := validateTestCommand(c); err == nil {
			t.Errorf("validateTestCommand(%q) = nil, want error", c)
		}
	}
}

// TestBuildTaskContextRejectsUnsafeProjects verifies that a crafted manifest
// (or a crafted --lang target) aborts the plan before any template renders.
func TestBuildTaskContextRejectsUnsafeProjects(t *testing.T) {
	tests := []struct {
		name    string
		project types.Project
	}{
		{"command in path", types.Project{Name: "evil", Path: "services/x; touch pwned", Language: "go"}},
		{"substitution in path", types.Project{Name: "evil", Path: "services/$(touch pwned)", Language: "go"}},
		{"newline in path", types.Project{Name: "evil", Path: "services/x\ntouch pwned", Language: "go"}},
		{"traversal in path", types.Project{Name: "evil", Path: "../outside", Language: "go"}},
		{"absolute path", types.Project{Name: "evil", Path: "/etc/passwd", Language: "go"}},
		{"backslash in path", types.Project{Name: "evil", Path: `services\api`, Language: "go"}},
		{"command in name", types.Project{Name: "evil; touch pwned", Path: "services/api", Language: "go"}},
		{"newline in name", types.Project{Name: "evil\ntouch pwned", Path: "services/api", Language: "go"}},
		{"yaml in name", types.Project{Name: `evil": {a: b}`, Path: "services/api", Language: "go"}},
		{"command in language", types.Project{Name: "evil", Path: "services/api", Language: "go); touch pwned; echo ($(x"}},
	}

	for _, tt := range tests {
		t.Run("existing/"+tt.name, func(t *testing.T) {
			m := &types.Manifest{Projects: []types.Project{tt.project}}
			_, cancel, err := buildTaskContext(RunContext{TargetPath: t.TempDir()}, m, &manifestChanges{}, nil)
			if cancel != nil {
				cancel()
			}
			if err == nil {
				t.Fatal("expected unsafe project to be rejected, got nil error")
			}
		})

		t.Run("added/"+tt.name, func(t *testing.T) {
			changes := &manifestChanges{Projects: []types.Project{tt.project}}
			_, cancel, err := buildTaskContext(RunContext{TargetPath: t.TempDir()}, &types.Manifest{}, changes, nil)
			if cancel != nil {
				cancel()
			}
			if err == nil {
				t.Fatal("expected unsafe added project to be rejected, got nil error")
			}
		})
	}
}

// TestBuildTaskContextRejectsUnsafeTestCommand verifies the free-form
// test_command metadata cannot carry a newline into the Makefile recipe.
func TestBuildTaskContextRejectsUnsafeTestCommand(t *testing.T) {
	m := &types.Manifest{
		Metadata: map[string]string{"test_command": "go test ./...\nrm -rf /"},
	}
	_, cancel, err := buildTaskContext(RunContext{TargetPath: t.TempDir()}, m, &manifestChanges{}, nil)
	if cancel != nil {
		cancel()
	}
	if err == nil {
		t.Fatal("expected unsafe test_command to be rejected, got nil error")
	}
}

// TestBuildTaskContextAcceptsBenignProjects ensures the new guard does not
// reject ordinary manifests.
func TestBuildTaskContextAcceptsBenignProjects(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{
			{Name: "backend", Path: "backend", Language: "go"},
			{Name: "web-app", Path: "services/web-app", Language: "svelte"},
			{Name: "root", Path: ".", Language: "go"},
		},
		Metadata: map[string]string{"test_command": "go test ./..."},
	}
	_, cancel, err := buildTaskContext(RunContext{TargetPath: t.TempDir()}, m, &manifestChanges{}, nil)
	if err != nil {
		t.Fatalf("benign manifest rejected: %v", err)
	}
	cancel()
}

// TestRepoRelativeProjectPath verifies that absolute targets are normalized to
// repo-relative paths so the manifest stays portable and the validator can
// require a repo-relative value.
func TestRepoRelativeProjectPath(t *testing.T) {
	tests := []struct {
		root, arg, want string
	}{
		{"/repo", "/repo/backend", "backend"},
		{"/repo", "/repo/services/api", "services/api"},
		{"/repo", "/repo", "."},
		{"/repo", "backend", "backend"},
		{"/repo", "./services/api", "services/api"},
		{"/repo", ".", "."},
		{"/repo", "/other/x", "../other/x"},
	}
	for _, tt := range tests {
		got, err := repoRelativeProjectPath(tt.root, tt.arg)
		if err != nil {
			t.Errorf("repoRelativeProjectPath(%q, %q) error: %v", tt.root, tt.arg, err)
			continue
		}
		if got != tt.want {
			t.Errorf("repoRelativeProjectPath(%q, %q) = %q, want %q", tt.root, tt.arg, got, tt.want)
		}
	}
	if _, err := repoRelativeProjectPath("/repo", ""); err == nil {
		t.Error("repoRelativeProjectPath with empty arg = nil error, want error")
	}
}
