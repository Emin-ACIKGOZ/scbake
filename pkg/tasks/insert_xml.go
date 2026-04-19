// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package tasks defines the executable units of work used in a scaffolding plan.
package tasks

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// InsertXMLTask inserts an XML fragment into an existing XML file at a specified path.
// It safely handles file modifications with transaction tracking for rollback.
type InsertXMLTask struct {
	// FilePath is the path to the XML file (relative to TargetPath)
	FilePath string

	// ElementPath is a simplified XPath indicating where to insert (e.g., "/project/build/plugins")
	ElementPath string

	// XMLContent is the XML fragment to insert (must be valid XML)
	XMLContent string

	// Desc is a human-readable description of the task
	Desc string

	// TaskPrio is the priority/order for execution
	TaskPrio int
}

// Description returns a human-readable summary of the task.
func (t *InsertXMLTask) Description() string {
	return t.Desc
}

// Priority returns the execution order (lower numbers first).
func (t *InsertXMLTask) Priority() int {
	return t.TaskPrio
}

// Execute performs the XML insertion with safety checks and transaction tracking.
func (t *InsertXMLTask) Execute(tc types.TaskContext) error {
	if tc.DryRun {
		return nil
	}

	absPath, err := t.validatePath(tc.TargetPath)
	if err != nil {
		return err
	}

	contentStr, err := t.readAndValidateXML(absPath)
	if err != nil {
		return err
	}

	if err := t.trackFile(tc, absPath); err != nil {
		return err
	}

	if t.isAlreadyInserted(contentStr) {
		return nil
	}

	newContent, err := insertXMLElement(contentStr, t.ElementPath, t.XMLContent)
	if err != nil {
		return fmt.Errorf("failed to insert XML into %s: %w", t.FilePath, err)
	}

	if err := os.WriteFile(absPath, []byte(newContent), fileutil.FilePerms); err != nil {
		return fmt.Errorf("failed to write modified XML file %s: %w", t.FilePath, err)
	}

	return nil
}

func (t *InsertXMLTask) validatePath(targetPath string) (string, error) {
	absPath, err := filepath.Abs(filepath.Join(targetPath, t.FilePath))
	if err != nil {
		return "", fmt.Errorf("failed to resolve path for %s: %w", t.FilePath, err)
	}

	absTarget, _ := filepath.Abs(targetPath)
	relPath, err := filepath.Rel(filepath.Clean(absTarget), filepath.Clean(absPath))
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path %s is outside target directory", t.FilePath)
	}

	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("XML file not found: %s", t.FilePath)
		}
		return "", fmt.Errorf("failed to stat XML file %s: %w", t.FilePath, err)
	}

	return absPath, nil
}

func (t *InsertXMLTask) readAndValidateXML(absPath string) (string, error) {
	// absPath is validated to be within target directory in validatePath()
	//nolint:gosec
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read XML file %s: %w", t.FilePath, err)
	}

	contentStr := string(content)
	if err := validateXML(contentStr); err != nil {
		return "", fmt.Errorf("XML file %s is malformed: %w", t.FilePath, err)
	}

	if err := validateXML(t.XMLContent); err != nil {
		return "", fmt.Errorf("XML content to insert is malformed: %w", err)
	}

	return contentStr, nil
}

func (t *InsertXMLTask) trackFile(tc types.TaskContext, absPath string) error {
	if tc.Tx != nil {
		if err := tc.Tx.Track(absPath); err != nil {
			return fmt.Errorf("failed to track file %s: %w", t.FilePath, err)
		}
	}
	return nil
}

func (t *InsertXMLTask) isAlreadyInserted(contentStr string) bool {
	normalizedNew := strings.TrimSpace(t.XMLContent)
	return containsNormalizedXML(contentStr, normalizedNew)
}

// containsNormalizedXML checks if normalized XML content exists in file (ignores whitespace differences).
func containsNormalizedXML(fileContent, xmlSnippet string) bool {
	// Split into lines and normalize each for comparison
	fileLines := strings.FieldsFunc(fileContent, func(r rune) bool { return r == '\n' })
	snippetLines := strings.FieldsFunc(xmlSnippet, func(r rune) bool { return r == '\n' })

	if len(snippetLines) == 0 {
		return false
	}

	// Look for the snippet in the file (allowing different indentation)
	for i := 0; i <= len(fileLines)-len(snippetLines); i++ {
		match := true
		for j, snippetLine := range snippetLines {
			if strings.TrimSpace(fileLines[i+j]) != strings.TrimSpace(snippetLine) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// validateXML checks that the given string is well-formed XML.
func validateXML(content string) error {
	decoder := xml.NewDecoder(strings.NewReader(content))
	// Set strict mode to catch malformed XML
	decoder.Strict = true
	for {
		_, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				return nil
			}
			return err
		}
	}
}

// insertXMLElement finds the target element path and inserts the XML content within it.
// For example, with path "/project/build/plugins" and existing file, it finds the <plugins>
// element and appends the new XML fragment before </plugins>.
func insertXMLElement(fileContent, elementPath, xmlToInsert string) (string, error) {
	// Parse element path (e.g., "/project/build/plugins" → ["project", "build", "plugins"])
	pathParts := strings.Split(strings.Trim(elementPath, "/"), "/")
	if len(pathParts) == 0 {
		return "", fmt.Errorf("invalid element path: %s", elementPath)
	}

	// Find the target element by searching for the deepest element opening tag
	targetElement := pathParts[len(pathParts)-1]
	openTag := "<" + targetElement + ">"
	openTagAlt := "<" + targetElement + " "
	closeTag := "</" + targetElement + ">"

	// Try to find the opening tag
	openIdx := strings.Index(fileContent, openTag)
	if openIdx == -1 {
		openIdx = strings.Index(fileContent, openTagAlt)
		if openIdx == -1 {
			return "", fmt.Errorf("element <%s> not found in XML", targetElement)
		}
		// Find the end of the opening tag when attributes are present
		closeGtIdx := strings.Index(fileContent[openIdx:], ">")
		if closeGtIdx == -1 {
			return "", fmt.Errorf("malformed opening tag for element <%s>", targetElement)
		}
		openIdx = openIdx + closeGtIdx + 1
	} else {
		openIdx = openIdx + len(openTag)
	}

	// Find the closing tag, searching only after the opening tag
	closeIdx := strings.Index(fileContent[openIdx:], closeTag)
	if closeIdx == -1 {
		return "", fmt.Errorf("closing tag </%s> not found in XML", targetElement)
	}
	// Adjust closeIdx to be absolute position
	closeIdx = openIdx + closeIdx

	// Insert the new content before the closing tag
	before := fileContent[:closeIdx]
	after := fileContent[closeIdx:]

	// Add proper indentation to the inserted content
	insertedLines := strings.Split(strings.TrimSpace(xmlToInsert), "\n")
	var indentedInsert strings.Builder
	indentedInsert.WriteString("    ")
	for i, line := range insertedLines {
		indentedInsert.WriteString(strings.TrimSpace(line))
		if i < len(insertedLines)-1 {
			indentedInsert.WriteString("\n    ")
		}
	}

	result := before + "\n" + indentedInsert.String() + "\n" + after

	return result, nil
}
