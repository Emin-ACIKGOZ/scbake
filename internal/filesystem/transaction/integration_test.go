// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package transaction_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"scbake/internal/core"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/ui"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
)

// fileSnapshot captures path, byte content, and file permissions of a file.
type fileSnapshot struct {
	path    string
	content []byte
	mode    os.FileMode
}

type priorityBandTestCase struct {
	name             string
	failPriorityBand types.Priority
	setupFunc        func(t *testing.T, rootDir string) (preExisting []fileSnapshot)
	buildTasks       func(rootDir string, preExisting []fileSnapshot) (plan *types.Plan, expectedCreated []string, lockfiles []string)
	failingTaskType  string
}

// collectDirectoryState returns a map of relative file/dir paths to a boolean (true if dir).
func collectDirectoryState(t *testing.T, rootDir string) map[string]bool {
	t.Helper()

	state := make(map[string]bool)
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		state[filepath.ToSlash(rel)] = d.IsDir()
		return nil
	})
	if err != nil {
		t.Fatalf("failed to collect directory state: %v", err)
	}
	return state
}

// TestPriorityBandFailures_Integration performs table-driven integration tests for the task execution engine
// and transaction manager across different execution priority bands (PrioLangSetup, PrioLinter, PrioDevEnv, PrioVersionControl).
//
// Verifies:
// 1. All created files and directories are deleted in reverse LIFO order.
// 2. Any modified files are restored to their exact pre-transaction byte content.
// 3. No orphan backup files or lockfiles remain on disk when an ExecCommandTask fails.
func TestPriorityBandFailures_Integration(t *testing.T) {
	tests := getPriorityBandTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runPriorityBandTestCase(t, tt)
		})
	}
}

func getPriorityBandTestCases() []priorityBandTestCase {
	return []priorityBandTestCase{
		{
			name:             "Failure at PrioLangSetup (Band 100-999)",
			failPriorityBand: types.PrioLangSetup,
			failingTaskType:  "ExecCommandTask",
			setupFunc:        setupLangSetupTest,
			buildTasks:       buildLangSetupTasks,
		},
		{
			name:             "Failure at PrioLinter (Band 1200-1399)",
			failPriorityBand: types.PrioLinter,
			failingTaskType:  "ExecCommandTask",
			setupFunc:        setupLinterTest,
			buildTasks:       buildLinterTasks,
		},
		{
			name:             "Failure at PrioDevEnv (Band 1500-1999)",
			failPriorityBand: types.PrioDevEnv,
			failingTaskType:  "ExecCommandTask",
			setupFunc:        setupDevEnvTest,
			buildTasks:       buildDevEnvTasks,
		},
		{
			name:             "Failure at PrioVersionControl (Band 2000-2100)",
			failPriorityBand: types.PrioVersionControl,
			failingTaskType:  "ExecCommandTask",
			setupFunc:        setupVersionControlTest,
			buildTasks:       buildVersionControlTasks,
		},
	}
}

func runPriorityBandTestCase(t *testing.T, tt priorityBandTestCase) {
	t.Helper()
	rootDir := t.TempDir()

	initialPreExisting := tt.setupFunc(t, rootDir)
	initialDirState := collectDirectoryState(t, rootDir)

	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to initialize transaction manager: %v", err)
	}

	plan, expectedCreatedPaths, predictedLockfiles := tt.buildTasks(rootDir, initialPreExisting)

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Tx:         tx,
	}

	reporter := ui.NewPlainReporter(len(plan.Tasks), false)
	execErr := core.Execute(plan, tc, reporter)

	if execErr == nil {
		t.Fatalf("Expected task execution to fail at priority band %v, but it succeeded", tt.failPriorityBand)
	}

	verifyUnreachableTasks(t, plan, tt.failPriorityBand)

	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatalf("Rollback failed with error: %v", rollbackErr)
	}

	verifyCreatedPathsDeleted(t, expectedCreatedPaths)
	verifyPreExistingFilesRestored(t, initialPreExisting)
	verifyDirectoryCleanliness(t, rootDir, initialDirState, predictedLockfiles)
}

func verifyUnreachableTasks(t *testing.T, plan *types.Plan, failBand types.Priority) {
	t.Helper()
	for _, task := range plan.Tasks {
		if task.Priority() > int(failBand) {
			if createDirTask, ok := task.(*tasks.CreateDirTask); ok {
				if _, err := os.Stat(createDirTask.Path); !os.IsNotExist(err) {
					t.Errorf("Task with priority %d executed despite earlier failure at priority %d", task.Priority(), failBand)
				}
			}
		}
	}
}

func verifyCreatedPathsDeleted(t *testing.T, expectedCreatedPaths []string) {
	t.Helper()
	for _, path := range expectedCreatedPaths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("[LIFO Deletion Check] Created path %s was not deleted by rollback", path)
		}
	}
}

func verifyPreExistingFilesRestored(t *testing.T, preExisting []fileSnapshot) {
	t.Helper()
	for _, initial := range preExisting {
		restoredContent, readErr := os.ReadFile(initial.path)
		if readErr != nil {
			t.Errorf("[Byte Content Check] Failed to read restored file %s: %v", initial.path, readErr)
			continue
		}

		if !bytes.Equal(restoredContent, initial.content) {
			t.Errorf("[Byte Content Check] Restored file %s content mismatch.\nWant (%d bytes): %q\nGot  (%d bytes): %q",
				initial.path, len(initial.content), initial.content, len(restoredContent), restoredContent)
		}

		if runtime.GOOS != "windows" {
			info, statErr := os.Stat(initial.path)
			if statErr != nil {
				t.Errorf("[File Mode Check] Failed to stat restored file %s: %v", initial.path, statErr)
			} else if info.Mode() != initial.mode {
				t.Errorf("[File Mode Check] Restored file %s mode mismatch. Want: %v, Got: %v", initial.path, initial.mode, info.Mode())
			}
		}
	}
}

func verifyDirectoryCleanliness(t *testing.T, rootDir string, initialDirState map[string]bool, predictedLockfiles []string) {
	t.Helper()
	for _, lockfile := range predictedLockfiles {
		if _, err := os.Stat(lockfile); !os.IsNotExist(err) {
			t.Errorf("[Orphan Check] Predicted lockfile %s still exists after rollback", lockfile)
		}
	}

	scbakeDir := filepath.Join(rootDir, fileutil.InternalDir)
	if _, err := os.Stat(scbakeDir); !os.IsNotExist(err) {
		t.Errorf("[Orphan Check] Transaction residue directory %s still exists on disk after rollback", scbakeDir)
	}

	postRollbackDirState := collectDirectoryState(t, rootDir)
	if len(postRollbackDirState) != len(initialDirState) {
		t.Errorf("[Cleanliness Check] Directory entry count mismatch after rollback.\nInitial (%d entries): %v\nPost-Rollback (%d entries): %v",
			len(initialDirState), mapKeys(initialDirState), len(postRollbackDirState), mapKeys(postRollbackDirState))
	}

	for relPath := range postRollbackDirState {
		if _, existed := initialDirState[relPath]; !existed {
			t.Errorf("[Cleanliness Check] Orphan file or directory found after rollback: %s", relPath)
		}
	}
}

// Setup & Task Builder Helpers for PrioLangSetup
func setupLangSetupTest(t *testing.T, rootDir string) []fileSnapshot {
	t.Helper()
	cfgPath := filepath.Join(rootDir, "config.json")
	binPath := filepath.Join(rootDir, "assets", "data.bin")

	cfgData := []byte(`{"app": "scbake", "version": "1.0.0"}`)
	binData := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x42, 0xFF, 0x7F}

	createTestFile(t, cfgPath, cfgData, fileutil.FilePerms)
	createTestFile(t, binPath, binData, fileutil.FilePerms)

	return []fileSnapshot{
		{path: cfgPath, content: cfgData, mode: fileutil.FilePerms},
		{path: binPath, content: binData, mode: fileutil.FilePerms},
	}
}

func buildLangSetupTasks(rootDir string, preExisting []fileSnapshot) (*types.Plan, []string, []string) {
	dir1 := filepath.Join(rootDir, "src")
	dir2 := filepath.Join(rootDir, "src", "pkg")
	dir3 := filepath.Join(rootDir, "src", "pkg", "internal")
	newFile := filepath.Join(rootDir, "src", "pkg", "internal", "service.go")

	lockfile1 := "go.sum"
	lockfile2 := "lang.lock"

	plan := &types.Plan{
		Tasks: []types.Task{
			&tasks.CreateDirTask{Path: dir1, Desc: "Create src", TaskPrio: int(types.PrioDirCreate)},
			&tasks.CreateDirTask{Path: dir2, Desc: "Create src/pkg", TaskPrio: int(types.PrioDirCreate) + 10},
			&tasks.CreateDirTask{Path: dir3, Desc: "Create src/pkg/internal", TaskPrio: int(types.PrioDirCreate) + 20},
			&tasks.AppendFileTask{FilePath: preExisting[0].path, Content: "\n// lang modification", Desc: "Modify config.json", TaskPrio: int(types.PrioLangSetup)},
			&tasks.AppendFileTask{FilePath: newFile, Content: "package internal\n", Desc: "Create service.go", TaskPrio: int(types.PrioLangSetup) + 10},
			&tasks.ExecCommandTask{
				Cmd:              "go",
				Args:             []string{"invalid_subcommand_xyz_999"},
				Desc:             "Failing Go command in PrioLangSetup",
				TaskPrio:         int(types.PrioLangSetup) + 50,
				RunInTarget:      true,
				PredictedCreated: []string{lockfile1, lockfile2},
			},
			&tasks.CreateDirTask{Path: filepath.Join(rootDir, "unreachable_linter"), Desc: "Unreachable Linter Task", TaskPrio: int(types.PrioLinter)},
		},
	}

	expectedCreated := []string{dir1, dir2, dir3, newFile}
	lockfiles := []string{filepath.Join(rootDir, lockfile1), filepath.Join(rootDir, lockfile2)}
	return plan, expectedCreated, lockfiles
}

// Setup & Task Builder Helpers for PrioLinter
func setupLinterTest(t *testing.T, rootDir string) []fileSnapshot {
	t.Helper()
	readmePath := filepath.Join(rootDir, "README.md")
	linterCfgPath := filepath.Join(rootDir, ".golangci.yml")

	readmeData := []byte("# Project Scbake\n\nInitial documentation.")
	linterCfgData := []byte("run:\n  timeout: 5m\n")

	createTestFile(t, readmePath, readmeData, fileutil.FilePerms)
	createTestFile(t, linterCfgPath, linterCfgData, fileutil.FilePerms)

	return []fileSnapshot{
		{path: readmePath, content: readmeData, mode: fileutil.FilePerms},
		{path: linterCfgPath, content: linterCfgData, mode: fileutil.FilePerms},
	}
}

func buildLinterTasks(rootDir string, preExisting []fileSnapshot) (*types.Plan, []string, []string) {
	ghDir := filepath.Join(rootDir, ".github")
	wfDir := filepath.Join(rootDir, ".github", "workflows")
	ciFile := filepath.Join(rootDir, ".github", "workflows", "ci.yml")
	mainFile := filepath.Join(rootDir, "main.go")

	cacheFile := ".golangci.cache"
	lockFile := "linter.lock"

	plan := &types.Plan{
		Tasks: []types.Task{
			&tasks.CreateDirTask{Path: ghDir, Desc: "Create .github", TaskPrio: int(types.PrioDirCreate)},
			&tasks.CreateDirTask{Path: wfDir, Desc: "Create .github/workflows", TaskPrio: int(types.PrioDirCreate) + 10},
			&tasks.AppendFileTask{FilePath: mainFile, Content: "package main\n\nfunc main() {}\n", Desc: "Create main.go", TaskPrio: int(types.PrioLangSetup)},
			&tasks.AppendFileTask{FilePath: preExisting[0].path, Content: "\n\nUpdated by universal config.", Desc: "Update README", TaskPrio: int(types.PrioConfigUniversal)},
			&tasks.AppendFileTask{FilePath: ciFile, Content: "name: CI\non: [push]\n", Desc: "Create ci.yml", TaskPrio: int(types.PrioCI)},
			&tasks.ExecCommandTask{
				Cmd:              "go",
				Args:             []string{"test", "--invalid-flag-xyz-12345"},
				Desc:             "Failing linter command in PrioLinter",
				TaskPrio:         int(types.PrioLinter),
				RunInTarget:      true,
				PredictedCreated: []string{cacheFile, lockFile},
			},
			&tasks.CreateDirTask{Path: filepath.Join(rootDir, ".devcontainer"), Desc: "Unreachable DevEnv Task", TaskPrio: int(types.PrioDevEnv)},
		},
	}

	expectedCreated := []string{ghDir, wfDir, mainFile, ciFile}
	lockfiles := []string{filepath.Join(rootDir, cacheFile), filepath.Join(rootDir, lockFile)}
	return plan, expectedCreated, lockfiles
}

// Setup & Task Builder Helpers for PrioDevEnv
func setupDevEnvTest(t *testing.T, rootDir string) []fileSnapshot {
	t.Helper()
	dcPath := filepath.Join(rootDir, "docker-compose.yml")
	pkgPath := filepath.Join(rootDir, "package.json")

	dcData := []byte("version: '3.8'\nservices:\n  app:\n    image: node:18-alpine\n")
	pkgData := []byte("{\n  \"name\": \"my-app\",\n  \"version\": \"1.0.0\"\n}\n")

	createTestFile(t, dcPath, dcData, fileutil.FilePerms)
	createTestFile(t, pkgPath, pkgData, fileutil.FilePerms)

	return []fileSnapshot{
		{path: dcPath, content: dcData, mode: fileutil.FilePerms},
		{path: pkgPath, content: pkgData, mode: fileutil.FilePerms},
	}
}

func buildDevEnvTasks(rootDir string, preExisting []fileSnapshot) (*types.Plan, []string, []string) {
	devDir := filepath.Join(rootDir, ".devcontainer")
	srcDir := filepath.Join(rootDir, "src")
	indexFile := filepath.Join(rootDir, "src", "index.js")
	ciDir := filepath.Join(rootDir, ".ci")
	linterDir := filepath.Join(rootDir, ".eslintrc.d")
	buildDir := filepath.Join(rootDir, "build")

	devLock := filepath.Join(".devcontainer", "devcontainer.lock")
	envLock := "dev.env"

	plan := &types.Plan{
		Tasks: []types.Task{
			&tasks.CreateDirTask{Path: devDir, Desc: "Create .devcontainer dir", TaskPrio: int(types.PrioDirCreate)},
			&tasks.CreateDirTask{Path: srcDir, Desc: "Create src dir", TaskPrio: int(types.PrioDirCreate) + 5},
			&tasks.AppendFileTask{FilePath: indexFile, Content: "console.log('hello');\n", Desc: "Create index.js", TaskPrio: int(types.PrioLangSetup)},
			&tasks.AppendFileTask{FilePath: preExisting[1].path, Content: "  \"scripts\": {\"test\": \"exit 0\"}\n", Desc: "Modify package.json", TaskPrio: int(types.PrioConfigUniversal)},
			&tasks.CreateDirTask{Path: ciDir, Desc: "Create .ci dir", TaskPrio: int(types.PrioCI)},
			&tasks.CreateDirTask{Path: linterDir, Desc: "Create .eslintrc.d", TaskPrio: int(types.PrioLinter)},
			&tasks.CreateDirTask{Path: buildDir, Desc: "Create build dir", TaskPrio: int(types.PrioBuildSystem)},
			&tasks.ExecCommandTask{
				Cmd:              "go",
				Args:             []string{"tool", "nonexistent_tool_xyz_999"},
				Desc:             "Failing dev container setup in PrioDevEnv",
				TaskPrio:         int(types.PrioDevEnv),
				RunInTarget:      true,
				PredictedCreated: []string{devLock, envLock},
			},
			&tasks.CreateDirTask{Path: filepath.Join(rootDir, ".git"), Desc: "Unreachable VCS Task", TaskPrio: int(types.PrioVersionControl)},
		},
	}

	expectedCreated := []string{devDir, srcDir, indexFile, ciDir, linterDir, buildDir}
	lockfiles := []string{filepath.Join(rootDir, devLock), filepath.Join(rootDir, envLock)}
	return plan, expectedCreated, lockfiles
}

// Setup & Task Builder Helpers for PrioVersionControl
func setupVersionControlTest(t *testing.T, rootDir string) []fileSnapshot {
	t.Helper()
	manifestPath := filepath.Join(rootDir, "scbake.json")
	mediaPath := filepath.Join(rootDir, "media", "logo.png")

	manifestData := []byte("{\n  \"version\": \"1.0.0\",\n  \"templates\": []\n}\n")
	mediaData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}

	createTestFile(t, manifestPath, manifestData, fileutil.FilePerms)
	createTestFile(t, mediaPath, mediaData, fileutil.FilePerms)

	return []fileSnapshot{
		{path: manifestPath, content: manifestData, mode: fileutil.FilePerms},
		{path: mediaPath, content: mediaData, mode: fileutil.FilePerms},
	}
}

func buildVersionControlTasks(rootDir string, preExisting []fileSnapshot) (*types.Plan, []string, []string) {
	d1 := filepath.Join(rootDir, "a")
	d2 := filepath.Join(rootDir, "a", "b")
	d3 := filepath.Join(rootDir, "a", "b", "c")
	d4 := filepath.Join(rootDir, "a", "b", "c", "d")
	f1 := filepath.Join(rootDir, "a", "b", "c", "d", "file.txt")

	gitLock := filepath.Join(".git", "LOCK")
	gitHead := filepath.Join(".git", "HEAD")

	plan := &types.Plan{
		Tasks: []types.Task{
			&tasks.CreateDirTask{Path: d1, Desc: "Create a", TaskPrio: int(types.PrioDirCreate)},
			&tasks.CreateDirTask{Path: d2, Desc: "Create a/b", TaskPrio: int(types.PrioDirCreate) + 5},
			&tasks.CreateDirTask{Path: d3, Desc: "Create a/b/c", TaskPrio: int(types.PrioDirCreate) + 10},
			&tasks.CreateDirTask{Path: d4, Desc: "Create a/b/c/d", TaskPrio: int(types.PrioDirCreate) + 15},
			&tasks.AppendFileTask{FilePath: f1, Content: "deep content\n", Desc: "Create file.txt", TaskPrio: int(types.PrioLangSetup)},
			&tasks.AppendFileTask{FilePath: preExisting[0].path, Content: "  // modified\n", Desc: "Modify scbake.json", TaskPrio: int(types.PrioConfigUniversal)},
			&tasks.ExecCommandTask{
				Cmd:              "git",
				Args:             []string{"init", "--invalid-flag-xyz-999"},
				Desc:             "Failing git init command in PrioVersionControl",
				TaskPrio:         int(types.PrioVersionControl),
				RunInTarget:      true,
				PredictedCreated: []string{gitLock, gitHead},
			},
		},
	}

	expectedCreated := []string{d1, d2, d3, d4, f1}
	lockfiles := []string{filepath.Join(rootDir, gitLock), filepath.Join(rootDir, gitHead)}
	return plan, expectedCreated, lockfiles
}

// TestStrictLIFOOrder_Integration explicitly asserts that directory trees are created in order
// and deleted in strict reverse LIFO order (deepest child path first, top-level directory last).
func TestStrictLIFOOrder_Integration(t *testing.T) {
	rootDir := t.TempDir()

	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to initialize transaction manager: %v", err)
	}

	topDir := filepath.Join(rootDir, "parent")
	midDir := filepath.Join(rootDir, "parent", "child")
	leafFile := filepath.Join(rootDir, "parent", "child", "leaf.txt")

	plan := &types.Plan{
		Tasks: []types.Task{
			&tasks.CreateDirTask{Path: topDir, Desc: "Create parent", TaskPrio: 50},
			&tasks.CreateDirTask{Path: midDir, Desc: "Create child", TaskPrio: 60},
			&tasks.AppendFileTask{FilePath: leafFile, Content: "leaf content", Desc: "Create leaf.txt", TaskPrio: 70},
			&tasks.ExecCommandTask{
				Cmd:         "go",
				Args:        []string{"invalid_cmd_xyz"},
				Desc:        "Trigger failure",
				TaskPrio:    80,
				RunInTarget: true,
			},
		},
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Tx:         tx,
	}

	reporter := ui.NewPlainReporter(len(plan.Tasks), false)
	_ = core.Execute(plan, tc, reporter)

	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatalf("Rollback failed: %v", rollbackErr)
	}

	if _, err := os.Stat(topDir); !os.IsNotExist(err) {
		t.Errorf("LIFO deletion failed: parent directory %s still exists", topDir)
	}
}

// Helper to create test file with parent directories
func createTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), fileutil.DirPerms); err != nil {
		t.Fatalf("failed to create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("failed to write test file %s: %v", path, err)
	}
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
