// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// testHelper provides reusable setup for InsertXMLTask tests.
type testHelper struct {
	tmpDir  string
	pomPath string
}

func newTestHelper(t *testing.T) *testHelper {
	tmpDir := t.TempDir()
	pomPath := filepath.Join(tmpDir, "pom.xml")

	// Create test pom.xml
	testPomContent, err := os.ReadFile(filepath.Join("testdata", "pom.xml"))
	if err != nil {
		t.Fatalf("Failed to read test fixture: %v", err)
	}

	//nolint:gosec // Path is constructed from t.TempDir() and hardcoded relative path
	if err := os.WriteFile(pomPath, testPomContent, fileutil.FilePerms); err != nil {
		t.Fatalf("Failed to setup test pom.xml: %v", err)
	}

	return &testHelper{tmpDir: tmpDir, pomPath: pomPath}
}

func (h *testHelper) taskContext(dryRun, force bool) types.TaskContext {
	return types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: h.tmpDir,
		Manifest:   &types.Manifest{SbakeVersion: "v1.0.0"},
		DryRun:     dryRun,
		Force:      force,
		Tx:         nil,
	}
}

func (h *testHelper) readPom() (string, error) {
	content, err := os.ReadFile(h.pomPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func TestInsertXMLTask_BasicInsertion(t *testing.T) {
	h := newTestHelper(t)

	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin><groupId>org.apache.maven.plugins</groupId><artifactId>maven-checkstyle-plugin</artifactId></plugin>`,
		Desc:        "Insert checkstyle plugin",
		TaskPrio:    1201,
	}

	if err := task.Execute(h.taskContext(false, false)); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	content, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom: %v", err)
	}

	if !strings.Contains(content, "maven-checkstyle-plugin") {
		t.Error("Plugin not inserted into pom.xml")
	}
}

func TestInsertXMLTask_DryRunNoModification(t *testing.T) {
	h := newTestHelper(t)
	originalContent, _ := h.readPom()

	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin><groupId>test</groupId></plugin>`,
		Desc:        "Test dry-run",
		TaskPrio:    1201,
	}

	if err := task.Execute(h.taskContext(true, false)); err != nil {
		t.Fatalf("Dry-run failed: %v", err)
	}

	afterContent, _ := h.readPom()
	if originalContent != afterContent {
		t.Error("Dry-run modified file when it shouldn't")
	}
}

func TestInsertXMLTask_Idempotent(t *testing.T) {
	h := newTestHelper(t)

	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin><groupId>org.example</groupId></plugin>`,
		Desc:        "Test idempotency",
		TaskPrio:    1201,
	}

	tc := h.taskContext(false, false)

	// First insertion
	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execution failed: %v", err)
	}

	firstContent, _ := h.readPom()
	pluginCount := strings.Count(firstContent, "<groupId>org.example</groupId>")

	// Second insertion (should not duplicate)
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Second execution failed: %v", err)
	}

	secondContent, _ := h.readPom()
	newPluginCount := strings.Count(secondContent, "<groupId>org.example</groupId>")

	if pluginCount != newPluginCount {
		t.Errorf("Plugin duplicated on second insertion: had %d, got %d", pluginCount, newPluginCount)
	}
}

func TestInsertXMLTask_PathTraversalRejected(t *testing.T) {
	h := newTestHelper(t)

	task := &InsertXMLTask{
		FilePath:    "../../etc/passwd",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin/>`,
		Desc:        "Test path traversal protection",
		TaskPrio:    1201,
	}

	if err := task.Execute(h.taskContext(false, false)); err == nil {
		t.Error("Path traversal should be rejected")
	}
}

func TestInsertXMLTask_FileNotFoundError(t *testing.T) {
	h := newTestHelper(t)

	task := &InsertXMLTask{
		FilePath:    "nonexistent.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin/>`,
		Desc:        "Test missing file",
		TaskPrio:    1201,
	}

	if err := task.Execute(h.taskContext(false, false)); err == nil {
		t.Error("Missing file should produce error")
	}
}

func TestInsertXMLTask_MalformedFileRejected(t *testing.T) {
	h := newTestHelper(t)

	// Overwrite with malformed XML
	badXML := filepath.Join(h.tmpDir, "bad.xml")
	if err := os.WriteFile(badXML, []byte("<root><unclosed>"), fileutil.FilePerms); err != nil {
		t.Fatalf("Failed to write bad XML: %v", err)
	}

	task := &InsertXMLTask{
		FilePath:    "bad.xml",
		ElementPath: "/root",
		XMLContent:  `<test/>`,
		Desc:        "Test malformed XML",
		TaskPrio:    1201,
	}

	if err := task.Execute(h.taskContext(false, false)); err == nil {
		t.Error("Malformed XML should be rejected")
	}
}

func TestInsertXMLTask_InvalidContentRejected(t *testing.T) {
	h := newTestHelper(t)

	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<unclosed>`,
		Desc:        "Test invalid content",
		TaskPrio:    1201,
	}

	if err := task.Execute(h.taskContext(false, false)); err == nil {
		t.Error("Invalid XML content should be rejected")
	}
}

func TestInsertXMLTask_MissingElementError(t *testing.T) {
	h := newTestHelper(t)

	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/nonexistent",
		XMLContent:  `<plugin/>`,
		Desc:        "Test missing element",
		TaskPrio:    1201,
	}

	if err := task.Execute(h.taskContext(false, false)); err == nil {
		t.Error("Missing target element should produce error")
	}
}

func TestInsertXMLTask_Description(t *testing.T) {
	task := &InsertXMLTask{Desc: "Test description"}
	if task.Description() != "Test description" {
		t.Errorf("Wrong description: %s", task.Description())
	}
}

func TestInsertXMLTask_Priority(t *testing.T) {
	task := &InsertXMLTask{TaskPrio: 1201}
	if task.Priority() != 1201 {
		t.Errorf("Wrong priority: %d", task.Priority())
	}
}

func TestInsertXMLTask_TransactionRollback(t *testing.T) {
	rootDir := t.TempDir()
	projectDir := filepath.Join(rootDir, "project")
	if err := os.Mkdir(projectDir, fileutil.DirPerms); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	pomPath := filepath.Join(projectDir, "pom.xml")
	testContent, err := os.ReadFile(filepath.Join("testdata", "pom.xml"))
	if err != nil {
		t.Fatalf("Failed to read test fixture: %v", err)
	}
	//nolint:gosec // Path is constructed from t.TempDir() and hardcoded filename
	if err := os.WriteFile(pomPath, testContent, fileutil.FilePerms); err != nil {
		t.Fatalf("Failed to write pom: %v", err)
	}
	originalContent := string(testContent)

	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}

	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin><groupId>test</groupId></plugin>`,
		Desc:        "Test transaction",
		TaskPrio:    1201,
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: projectDir,
		Manifest:   &types.Manifest{SbakeVersion: "v1.0.0"},
		DryRun:     false,
		Force:      false,
		Tx:         tx,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Task execution failed: %v", err)
	}

	//nolint:gosec // Path is from t.TempDir() and hardcoded filename
	modified, _ := os.ReadFile(pomPath)
	if string(modified) == originalContent {
		t.Error("File should be modified")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	//nolint:gosec // Path is from t.TempDir() and hardcoded filename
	restored, _ := os.ReadFile(pomPath)
	if string(restored) != originalContent {
		t.Error("Rollback should restore original content")
	}
}

func TestValidateXML_ValidDocuments(t *testing.T) {
	tests := []string{
		`<root/>`,
		`<root><child/></root>`,
		`<root xmlns="http://example.com"><child attr="val"/></root>`,
		`<?xml version="1.0"?><root/>`,
	}

	for _, xml := range tests {
		if err := validateXML(xml); err != nil {
			t.Errorf("Valid XML rejected: %s", xml)
		}
	}
}

func TestValidateXML_InvalidDocuments(t *testing.T) {
	tests := []string{
		`<root><unclosed>`,
		`<root></root`,
		`<root attr="value"`,
		`<>`,
	}

	for _, xml := range tests {
		if err := validateXML(xml); err == nil {
			t.Errorf("Invalid XML accepted: %s", xml)
		}
	}
}

func TestInsertXMLElement_ComplexStructure(t *testing.T) {
	fileContent := `<project>
	<build>
		<plugins>
			<plugin>
				<groupId>existing</groupId>
			</plugin>
		</plugins>
	</build>
</project>`

	result, err := insertXMLElement(fileContent, "/project/build/plugins", `<plugin><groupId>new</groupId></plugin>`)
	if err != nil {
		t.Fatalf("Insertion failed: %v", err)
	}

	if !strings.Contains(result, "existing") || !strings.Contains(result, "new") {
		t.Error("Insertion lost existing or new content")
	}

	if !strings.Contains(result, "</plugins>") {
		t.Error("Element structure not preserved")
	}
}

func TestInsertXMLElement_WithAttributes(t *testing.T) {
	fileContent := `<root xmlns="http://example.com" version="1.0">
	<plugins>
		<existing/>
	</plugins>
</root>`

	result, err := insertXMLElement(fileContent, "/root/plugins", `<new/>`)
	if err != nil {
		t.Fatalf("Insertion failed: %v", err)
	}

	if !strings.Contains(result, `xmlns="http://example.com"`) {
		t.Error("Attributes not preserved")
	}

	if !strings.Contains(result, "<new/>") {
		t.Error("New element not inserted")
	}
}
