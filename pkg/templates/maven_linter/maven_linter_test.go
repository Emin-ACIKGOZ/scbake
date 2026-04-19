// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: gpl3-or-later

package mavenlinter

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

func TestHandler_GetTasks(t *testing.T) {
	handler := &Handler{}
	taskList, err := handler.GetTasks(".")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	// Should return 2 tasks: CreateTemplateTask for checkstyle.xml + InsertXMLTask for pom.xml
	if len(taskList) != 2 {
		t.Errorf("Expected 2 tasks, got %d", len(taskList))
	}

	// Task 1 should be CreateTemplateTask
	if _, ok := taskList[0].(*tasks.CreateTemplateTask); !ok {
		t.Errorf("Task 1 should be CreateTemplateTask, got %T", taskList[0])
	}

	// Task 2 should be InsertXMLTask
	if _, ok := taskList[1].(*tasks.InsertXMLTask); !ok {
		t.Errorf("Task 2 should be InsertXMLTask, got %T", taskList[1])
	}
}

func TestHandler_TaskPriorities(t *testing.T) {
	handler := &Handler{}
	taskList, err := handler.GetTasks(".")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	// Priorities should be sequential in Linter band (1200, 1201)
	p1 := taskList[0].Priority()
	p2 := taskList[1].Priority()

	if p1 != 1200 {
		t.Errorf("Task 1 priority: expected 1200, got %d", p1)
	}

	if p2 != 1201 {
		t.Errorf("Task 2 priority: expected 1201, got %d", p2)
	}

	if p2 <= p1 {
		t.Errorf("Task 2 priority should be higher than task 1")
	}
}

func TestHandler_TaskDescriptions(t *testing.T) {
	handler := &Handler{}
	taskList, err := handler.GetTasks(".")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	desc1 := taskList[0].Description()
	desc2 := taskList[1].Description()

	if desc1 != "Create Maven Checkstyle configuration" {
		t.Errorf("Task 1 description: got %q", desc1)
	}

	if desc2 != "Inject Maven Checkstyle plugin into pom.xml" {
		t.Errorf("Task 2 description: got %q", desc2)
	}
}

// testExecutionHelper provides setup and cleanup for full execution tests.
type testExecutionHelper struct {
	tmpDir     string
	projectDir string
	pomPath    string
}

func newTestExecutionHelper(t *testing.T) *testExecutionHelper {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")

	if err := os.Mkdir(projectDir, fileutil.DirPerms); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	testPomPath := filepath.Join("../../tasks/testdata", "pom.xml")
	//nolint:gosec // Path is hardcoded relative to test
	pomContent, err := os.ReadFile(testPomPath)
	if err != nil {
		t.Fatalf("Failed to read test pom.xml: %v", err)
	}

	pomPath := filepath.Join(projectDir, "pom.xml")
	//nolint:gosec // Path is constructed from t.TempDir() and hardcoded filename
	if err := os.WriteFile(pomPath, pomContent, fileutil.FilePerms); err != nil {
		t.Fatalf("Failed to write pom.xml: %v", err)
	}

	return &testExecutionHelper{
		tmpDir:     tmpDir,
		projectDir: projectDir,
		pomPath:    pomPath,
	}
}

func (h *testExecutionHelper) executeTasks(t *testing.T) {
	handler := &Handler{}
	taskList, err := handler.GetTasks(h.projectDir)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	tx, err := transaction.New(h.tmpDir)
	if err != nil {
		t.Fatalf("Failed to create transaction manager: %v", err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: h.projectDir,
		Manifest:   &types.Manifest{SbakeVersion: "v1.0.0"},
		DryRun:     false,
		Force:      false,
		Tx:         tx,
	}

	for _, task := range taskList {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Task execution failed: %v", err)
		}
	}
}

func (h *testExecutionHelper) readPom() (string, error) {
	content, err := os.ReadFile(h.pomPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func TestHandler_FullExecution_CheckstyleFileCreated(t *testing.T) {
	h := newTestExecutionHelper(t)
	h.executeTasks(t)

	checkstylePath := filepath.Join(h.projectDir, "checkstyle.xml")
	if _, err := os.Stat(checkstylePath); err != nil {
		t.Fatalf("checkstyle.xml not created: %v", err)
	}
}

func TestHandler_FullExecution_PluginInjected(t *testing.T) {
	h := newTestExecutionHelper(t)
	h.executeTasks(t)

	pomContent, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom.xml: %v", err)
	}

	if !strings.Contains(pomContent, "maven-checkstyle-plugin") {
		t.Error("maven-checkstyle-plugin not found in pom.xml")
	}
}

func TestHandler_FullExecution_PluginGroupId(t *testing.T) {
	h := newTestExecutionHelper(t)
	h.executeTasks(t)

	pomContent, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom.xml: %v", err)
	}

	if !strings.Contains(pomContent, "org.apache.maven.plugins") {
		t.Error("Plugin groupId not found in pom.xml")
	}
}

func TestHandler_FullExecution_ConfigurationReference(t *testing.T) {
	h := newTestExecutionHelper(t)
	h.executeTasks(t)

	pomContent, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom.xml: %v", err)
	}

	if !strings.Contains(pomContent, "checkstyle.xml") {
		t.Error("Configuration reference to checkstyle.xml not found")
	}
}

func TestHandler_FullExecution_NoOrphanedFile(t *testing.T) {
	h := newTestExecutionHelper(t)
	h.executeTasks(t)

	orphanedPath := filepath.Join(h.projectDir, "maven-checkstyle-plugin.xml")
	if _, err := os.Stat(orphanedPath); err == nil {
		t.Error("Orphaned maven-checkstyle-plugin.xml file should not exist")
	}
}

func TestHandler_PluginSnippetContent(t *testing.T) {
	handler := &Handler{}
	taskList, err := handler.GetTasks(".")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	insertTask, ok := taskList[1].(*tasks.InsertXMLTask)
	if !ok {
		t.Fatalf("Task 2 should be InsertXMLTask")
	}

	// Check that XML content is not empty and contains expected elements
	if insertTask.XMLContent == "" {
		t.Error("XML content is empty")
	}

	if !strings.Contains(insertTask.XMLContent, "maven-checkstyle-plugin") {
		t.Error("XML content should contain maven-checkstyle-plugin")
	}

	if !strings.Contains(insertTask.XMLContent, "org.apache.maven.plugins") {
		t.Error("XML content should contain plugin groupId")
	}

	if !strings.Contains(insertTask.XMLContent, "checkstyle.xml") {
		t.Error("XML content should reference checkstyle.xml")
	}
}
