// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: gpl3-or-later

package integration

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/pkg/templates"
)

// mavenTestHelper provides setup and execution utilities for Maven Linter integration tests.
type mavenTestHelper struct {
	tmpDir  string
	pomPath string
}

func newMavenTestHelper(t *testing.T) *mavenTestHelper {
	tmpDir := t.TempDir()

	testPomPath := filepath.Join("../pkg/tasks/testdata", "pom.xml")
	//nolint:gosec // Path is hardcoded relative to test
	pomContent, err := os.ReadFile(testPomPath)
	if err != nil {
		t.Fatalf("Failed to read test pom.xml: %v", err)
	}

	pomPath := filepath.Join(tmpDir, "pom.xml")
	//nolint:gosec // Path is constructed from t.TempDir() and hardcoded filename
	if err := os.WriteFile(pomPath, pomContent, fileutil.FilePerms); err != nil {
		t.Fatalf("Failed to write pom.xml: %v", err)
	}

	return &mavenTestHelper{
		tmpDir:  tmpDir,
		pomPath: pomPath,
	}
}

func (h *mavenTestHelper) executeTasks(t *testing.T, dryRun, force bool) {
	handler, err := templates.GetHandler("maven_linter")
	if err != nil {
		t.Fatalf("Failed to get maven_linter handler: %v", err)
	}

	tasks, err := handler.GetTasks(h.tmpDir)
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: h.tmpDir,
		Manifest:   &types.Manifest{SbakeVersion: "v1.0.0"},
		DryRun:     dryRun,
		Force:      force,
		Tx:         nil,
	}

	for i, task := range tasks {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Task %d execution failed: %v", i, err)
		}
	}
}

func (h *mavenTestHelper) executeTasksWithTx(t *testing.T) *transaction.Manager {
	handler, err := templates.GetHandler("maven_linter")
	if err != nil {
		t.Fatalf("Failed to get maven_linter handler: %v", err)
	}

	tasks, err := handler.GetTasks(h.tmpDir)
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}

	tx, err := transaction.New(h.tmpDir)
	if err != nil {
		t.Fatalf("Failed to create transaction manager: %v", err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: h.tmpDir,
		Manifest:   &types.Manifest{SbakeVersion: "v1.0.0"},
		DryRun:     false,
		Force:      false,
		Tx:         tx,
	}

	for _, task := range tasks {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Task execution failed: %v", err)
		}
	}

	return tx
}

func (h *mavenTestHelper) readPom() (string, error) {
	content, err := os.ReadFile(h.pomPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func TestMavenLinterHandlerIntegration_CheckstyleCreated(t *testing.T) {
	h := newMavenTestHelper(t)
	h.executeTasks(t, false, false)

	checkstylePath := filepath.Join(h.tmpDir, "checkstyle.xml")
	if _, err := os.Stat(checkstylePath); err != nil {
		t.Fatalf("checkstyle.xml not created: %v", err)
	}
}

func TestMavenLinterHandlerIntegration_PluginInjected(t *testing.T) {
	h := newMavenTestHelper(t)
	h.executeTasks(t, false, false)

	pomStr, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom.xml: %v", err)
	}

	if !strings.Contains(pomStr, "maven-checkstyle-plugin") {
		t.Error("maven-checkstyle-plugin not found in pom.xml")
	}
}

func TestMavenLinterHandlerIntegration_PluginGroupId(t *testing.T) {
	h := newMavenTestHelper(t)
	h.executeTasks(t, false, false)

	pomStr, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom.xml: %v", err)
	}

	if !strings.Contains(pomStr, "org.apache.maven.plugins") {
		t.Error("Plugin groupId not found")
	}
}

func TestMavenLinterHandlerIntegration_NoOrphanedFile(t *testing.T) {
	h := newMavenTestHelper(t)
	h.executeTasks(t, false, false)

	orphanedPath := filepath.Join(h.tmpDir, "maven-checkstyle-plugin.xml")
	if _, err := os.Stat(orphanedPath); err == nil {
		t.Error("Orphaned maven-checkstyle-plugin.xml should not exist")
	}
}

func TestMavenLinterHandlerIntegration_XMLValid(t *testing.T) {
	h := newMavenTestHelper(t)
	h.executeTasks(t, false, false)

	pomContent, err := os.ReadFile(h.pomPath)
	if err != nil {
		t.Fatalf("Failed to read pom.xml: %v", err)
	}

	var pomProject struct {
		XMLName xml.Name
	}

	if err := xml.Unmarshal(pomContent, &pomProject); err != nil {
		t.Fatalf("Modified pom.xml is not valid XML: %v", err)
	}
}

func TestMavenLinterIdempotency_NoDuplication(t *testing.T) {
	h := newMavenTestHelper(t)
	h.executeTasks(t, false, false)

	pomAfterFirst, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read after first execution: %v", err)
	}

	h.executeTasks(t, false, true)

	pomAfterSecond, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read after second execution: %v", err)
	}

	firstCount := strings.Count(pomAfterFirst, "maven-checkstyle-plugin")
	secondCount := strings.Count(pomAfterSecond, "maven-checkstyle-plugin")

	if firstCount != secondCount {
		t.Errorf("Plugin duplicated. First: %d, Second: %d", firstCount, secondCount)
	}
}

func TestMavenLinterIdempotency_SingleOccurrence(t *testing.T) {
	h := newMavenTestHelper(t)
	h.executeTasks(t, false, false)

	pomContent, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom.xml: %v", err)
	}

	count := strings.Count(pomContent, "maven-checkstyle-plugin")
	if count != 1 {
		t.Errorf("Expected 1 occurrence, got %d", count)
	}
}

func TestMavenLinterIdempotency_ContentUnchanged(t *testing.T) {
	h := newMavenTestHelper(t)
	h.executeTasks(t, false, false)

	pomAfterFirst, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read after first execution: %v", err)
	}

	h.executeTasks(t, false, true)

	pomAfterSecond, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read after second execution: %v", err)
	}

	if pomAfterFirst != pomAfterSecond {
		t.Error("File changed on second execution (not idempotent)")
	}
}

func TestMavenLinterRollback_ModifiesFile(t *testing.T) {
	h := newMavenTestHelper(t)
	originalContent, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read original pom.xml: %v", err)
	}

	h.executeTasksWithTx(t)

	modifiedContent, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read modified pom.xml: %v", err)
	}

	if modifiedContent == originalContent {
		t.Error("pom.xml was not modified")
	}
}

func TestMavenLinterRollback_RestoresOriginal(t *testing.T) {
	h := newMavenTestHelper(t)
	originalContent, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read original pom.xml: %v", err)
	}

	tx := h.executeTasksWithTx(t)

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	rolledBackContent, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read after rollback: %v", err)
	}

	if rolledBackContent != originalContent {
		t.Error("Rollback did not restore original content")
	}
}

// TestMavenLinterMissingPluginsSection tests error handling for missing plugins section.
func TestMavenLinterMissingPluginsSection(t *testing.T) {
	tmpDir := t.TempDir()

	// Create minimal pom.xml without plugins section
	minimalPom := `<?xml version="1.0"?>
<project>
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.example</groupId>
    <artifactId>demo</artifactId>
    <version>1.0</version>
</project>`

	pomPath := filepath.Join(tmpDir, "pom.xml")
	if err := os.WriteFile(pomPath, []byte(minimalPom), fileutil.FilePerms); err != nil {
		t.Fatalf("Failed to write pom.xml: %v", err)
	}

	handler, err := templates.GetHandler("maven_linter")
	if err != nil {
		t.Fatalf("Failed to get maven_linter handler: %v", err)
	}

	tasks, err := handler.GetTasks(tmpDir)
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		Manifest:   &types.Manifest{SbakeVersion: "v1.0.0"},
		DryRun:     false,
		Force:      false,
		Tx:         nil,
	}

	// Checkstyle task should succeed
	if err := tasks[0].Execute(tc); err != nil {
		t.Fatalf("Checkstyle task failed: %v", err)
	}

	// Insert task should fail (no plugins section)
	if err := tasks[1].Execute(tc); err == nil {
		t.Error("Expected error when plugins section missing, got none")
	}
}
