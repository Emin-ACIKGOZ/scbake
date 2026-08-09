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

func TestInsertXMLTask_Boundary_MalformedUnclosedTargetXML(t *testing.T) {
	h := newTestHelper(t)

	malformedCases := []struct {
		name    string
		content string
	}{
		{"unclosed root tag", "<project><build><plugins></build></project>"},
		{"unclosed child tag", "<project><build><plugins><plugin name='foo'></plugins></build></project>"},
		{"mismatched closing tag", "<project><build></other></project>"},
		{"truncated tag", "<project><build><plu"},
		{"bad attribute quotes", `<project><build attr="val></project>`},
		{"unclosed element tag", "<project><build><plugins>"},
		{"raw dangling bracket", "<project><build><plugins><</plugins></build></project>"},
	}

	for _, tc := range malformedCases {
		t.Run(tc.name, func(t *testing.T) {
			targetFile := filepath.Join(h.tmpDir, "target.xml")
			if err := os.WriteFile(targetFile, []byte(tc.content), fileutil.FilePerms); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			task := &InsertXMLTask{
				FilePath:    "target.xml",
				ElementPath: "/project/build/plugins",
				XMLContent:  `<plugin><groupId>com.example</groupId><artifactId>test-plugin</artifactId></plugin>`,
				Desc:        "Test malformed target XML boundary",
				TaskPrio:    1201,
			}

			err := task.Execute(h.taskContext(false, false))
			if err == nil {
				t.Errorf("Expected error for malformed target XML case %q, but got nil", tc.name)
			}

			// Verify file content was not corrupted
			//nolint:gosec // Path is in t.TempDir()
			after, readErr := os.ReadFile(targetFile)
			if readErr != nil {
				t.Fatalf("Failed to read file after execution attempt: %v", readErr)
			}
			if string(after) != tc.content {
				t.Errorf("File content was modified during failed execution for case %q", tc.name)
			}
		})
	}
}

func TestInsertXMLTask_Boundary_DeeplyNestedNamespaces(t *testing.T) {
	h := newTestHelper(t)

	deepXML := `<?xml version="1.0" encoding="UTF-8"?>
<ns1:root xmlns:ns1="http://example.com/schema/root"
          xmlns:ns2="http://example.com/schema/level2"
          xmlns:ns3="http://example.com/schema/level3"
          xmlns:ns4="http://example.com/schema/level4"
          xmlns:ns5="http://example.com/schema/level5"
          xmlns:ns6="http://example.com/schema/level6"
          xmlns:ns7="http://example.com/schema/level7"
          xmlns:ns8="http://example.com/schema/level8"
          xmlns:ns9="http://example.com/schema/level9"
          xmlns:ns10="http://example.com/schema/level10">
	<ns2:level2 attr="val2">
		<ns3:level3 attr="val3">
			<ns4:level4 attr="val4">
				<ns5:level5 attr="val5">
					<ns6:level6 attr="val6">
						<ns7:level7 attr="val7">
							<ns8:level8 attr="val8">
								<ns9:level9 attr="val9">
									<ns10:plugins defaultAttr="active">
										<ns10:plugin id="existing-1"/>
									</ns10:plugins>
								</ns9:level9>
							</ns8:level8>
						</ns7:level7>
					</ns6:level6>
				</ns5:level5>
			</ns4:level4>
		</ns3:level3>
	</ns2:level2>
</ns1:root>`

	targetFile := filepath.Join(h.tmpDir, "deep_ns.xml")
	if err := os.WriteFile(targetFile, []byte(deepXML), fileutil.FilePerms); err != nil {
		t.Fatalf("Failed to write deep ns file: %v", err)
	}

	task := &InsertXMLTask{
		FilePath:    "deep_ns.xml",
		ElementPath: "/ns1:root/ns2:level2/ns3:level3/ns4:level4/ns5:level5/ns6:level6/ns7:level7/ns8:level8/ns9:level9/ns10:plugins",
		XMLContent:  `<ns10:plugin id="new-inserted-plugin"/>`,
		Desc:        "Test deeply nested namespace insertion",
		TaskPrio:    1201,
	}

	if err := task.Execute(h.taskContext(false, false)); err != nil {
		t.Fatalf("Execute failed for deeply nested namespaces: %v", err)
	}

	//nolint:gosec // Path is in t.TempDir()
	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, `<ns10:plugin id="new-inserted-plugin"/>`) {
		t.Error("New namespaced element not found in file")
	}

	if !strings.Contains(contentStr, `<ns10:plugin id="existing-1"/>`) {
		t.Error("Existing namespaced element was lost")
	}

	if err := validateXML(contentStr); err != nil {
		t.Errorf("Modified namespaced XML is invalid: %v", err)
	}
}

func TestInsertXMLTask_Boundary_MissingTargetXPaths(t *testing.T) {
	missingCases := []struct {
		name string
		path string
	}{
		{"nonexistent leaf", "/project/build/nonexistent"},
		{"nonexistent parent", "/project/nonexistent/plugins"},
		{"nonexistent root", "/missingroot/build/plugins"},
		{"empty path", ""},
		{"root slash only", "/"},
		{"multiple slashes", "///"},
		{"invalid node name", "/project/build/123invalid!"},
	}

	for _, tc := range missingCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHelper(t)
			task := &InsertXMLTask{
				FilePath:    "pom.xml",
				ElementPath: tc.path,
				XMLContent:  `<plugin><groupId>org.unique.missing.test</groupId></plugin>`,
				Desc:        "Test missing XPath target",
				TaskPrio:    1201,
			}

			err := task.Execute(h.taskContext(false, false))
			if err == nil {
				t.Errorf("Expected error for missing target XPath %q, got nil", tc.path)
			}
		})
	}
}

func TestInsertXMLTask_Boundary_AncestorXPathMatchingBehavior(t *testing.T) {
	// Security Contract: For an invalid path such as /project/nonexistent/plugins,
	// Execute() MUST return an error and the target XML MUST remain unchanged.
	h := newTestHelper(t)
	originalContent, readErr := h.readPom()
	if readErr != nil {
		t.Fatalf("Failed to read initial pom.xml: %v", readErr)
	}

	snippet := `<plugin><groupId>org.ancestor.contract.test</groupId></plugin>`
	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/nonexistent/plugins",
		XMLContent:  snippet,
		Desc:        "Test ancestor XPath matching security contract",
		TaskPrio:    1201,
	}

	err := task.Execute(h.taskContext(false, false))
	if err == nil {
		t.Error("Security Contract Violation: Execute() should return an error when target XPath contains nonexistent parent tags (/project/nonexistent/plugins)")
	}

	content, readErr := h.readPom()
	if readErr != nil {
		t.Fatalf("Failed to read pom file: %v", readErr)
	}

	if content != originalContent {
		t.Error("Security Contract Violation: Target XML file was modified despite invalid ancestor XPath")
	}
}

func TestInsertXMLTask_Boundary_DuplicateExecution(t *testing.T) {
	h := newTestHelper(t)

	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent: `<plugin>
	<groupId>com.security.qa</groupId>
	<artifactId>idempotency-checker</artifactId>
	<!-- Security verification tag -->
</plugin>`,
		Desc:     "Test idempotency across repeated runs",
		TaskPrio: 1201,
	}

	tc := h.taskContext(false, false)

	// First execution
	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execution failed: %v", err)
	}

	content1, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom after 1st execution: %v", err)
	}

	count1 := strings.Count(content1, "<artifactId>idempotency-checker</artifactId>")
	if count1 != 1 {
		t.Fatalf("Expected exactly 1 occurrence after first run, got %d", count1)
	}

	// Second execution (should be no-op)
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Second execution failed: %v", err)
	}

	content2, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom after 2nd execution: %v", err)
	}

	if content1 != content2 {
		t.Error("File content changed on second execution (idempotency violation)")
	}

	// Third execution
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Third execution failed: %v", err)
	}

	content3, err := h.readPom()
	if err != nil {
		t.Fatalf("Failed to read pom after 3rd execution: %v", err)
	}

	if content1 != content3 {
		t.Error("File content changed on third execution (idempotency violation)")
	}
}

func TestInsertXMLTask_Boundary_PathTraversalValidation(t *testing.T) {
	h := newTestHelper(t)

	traversalPayloads := []string{
		"../../etc/passwd",
		"../../../etc/shadow",
		"subdir/../../secret.txt",
		"/etc/passwd",
		"..\\..\\Windows\\System32\\drivers\\etc\\hosts",
		"pom.xml/../../etc/hosts",
	}

	for _, payload := range traversalPayloads {
		t.Run(payload, func(t *testing.T) {
			task := &InsertXMLTask{
				FilePath:    payload,
				ElementPath: "/project/build/plugins",
				XMLContent:  `<plugin/>`,
				Desc:        "Path traversal safety test",
				TaskPrio:    1201,
			}

			err := task.Execute(h.taskContext(false, false))
			if err == nil {
				t.Errorf("Path traversal payload %q was unexpectedly accepted", payload)
			}
		})
	}
}
