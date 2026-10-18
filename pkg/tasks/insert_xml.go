// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
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
	// Validate the target file path and the element path before any mode-specific
	// handling, so dry-run and real runs reject invalid input identically.
	absPath, err := t.validatePath(tc.TargetPath)
	if err != nil {
		return err
	}

	if _, err := parseElementPath(t.ElementPath); err != nil {
		return err
	}

	if tc.DryRun {
		return nil
	}

	contentStr, err := t.readAndValidateXML(tc.TargetPath)
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

	if err := fileutil.SafeWriteFile(tc.TargetPath, t.FilePath, []byte(newContent), fileutil.FilePerms); err != nil {
		return fmt.Errorf("failed to write modified XML file %s: %w", t.FilePath, err)
	}

	return nil
}

func (t *InsertXMLTask) validatePath(targetPath string) (string, error) {
	// fileutil.SafePath performs both lexical and symlink-aware containment, so
	// a file reached through a symlink that escapes the target is rejected.
	absPath, err := fileutil.SafePath(targetPath, t.FilePath)
	if err != nil {
		return "", fmt.Errorf("path %s is outside target directory: %w", t.FilePath, err)
	}

	// Lstat: do not follow a symlink. The final component is later opened with
	// O_NOFOLLOW, but rejecting here reports a clear error and prevents reading
	// through a link.
	if info, err := os.Lstat(absPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("XML file not found: %s", t.FilePath)
		}
		return "", fmt.Errorf("failed to stat XML file %s: %w", t.FilePath, err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("XML file %s is a symlink, which is not allowed", t.FilePath)
	}

	return absPath, nil
}

func (t *InsertXMLTask) readAndValidateXML(targetPath string) (string, error) {
	// The path is validated by validatePath() and SafeReadFile re-checks
	// containment, opening the final component with O_NOFOLLOW.
	content, err := fileutil.SafeReadFile(targetPath, t.FilePath)
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
// Uses O(n) single-pass algorithm instead of nested loops.
func containsNormalizedXML(fileContent, xmlSnippet string) bool {
	normalizedSnippet := strings.TrimSpace(xmlSnippet)
	if normalizedSnippet == "" {
		return false
	}

	// Split into lines once
	snippetLines := strings.Split(normalizedSnippet, "\n")
	if len(snippetLines) == 0 {
		return false
	}

	// Normalize snippet lines once
	normalizedSnippetLines := make([]string, len(snippetLines))
	for i, line := range snippetLines {
		normalizedSnippetLines[i] = strings.TrimSpace(line)
	}

	// Split file into lines
	fileLines := strings.Split(fileContent, "\n")
	if len(fileLines) < len(normalizedSnippetLines) {
		return false
	}

	// Single pass through file
	for i := 0; i <= len(fileLines)-len(normalizedSnippetLines); i++ {
		match := true
		for j, snippetLine := range normalizedSnippetLines {
			if strings.TrimSpace(fileLines[i+j]) != snippetLine {
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
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// parseElementPath splits a simplified element path (e.g. "/project/build/plugins")
// into its segments. Empty segments (from repeated slashes) are ignored; a path
// with no segments at all is rejected.
func parseElementPath(elementPath string) ([]string, error) {
	var segments []string
	for _, s := range strings.Split(strings.Trim(elementPath, "/"), "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("invalid element path: %q", elementPath)
	}
	return segments, nil
}

// xmlFrame tracks one open element while locating an insertion point.
type xmlFrame struct {
	// name is the element name as reported by xml.Decoder.Token (Space is the
	// namespace URL, Local the local name).
	name xml.Name
	// ns is the scoped namespace environment (prefix → URL, "" for the default
	// namespace) in effect for this element.
	ns map[string]string
	// matched is true when this element and all of its ancestors matched the
	// requested path.
	matched bool
}

// locateXMLElement walks the XML structure of content and returns the byte
// offset at which an insertion must be spliced: immediately before the closing
// tag of the first element whose full ancestry matches segments.
//
// Namespace handling is the repository's ElementPath contract, not XPath: a
// prefixed segment (prefix:local) resolves its prefix against the scoped xmlns
// environment; an unprefixed segment matches the default namespace in scope.
//
// Self-closing targets are rejected: Token() expands them into a synthesized
// EndElement that does not advance the input position, so its token span is
// zero-width. This is an observed property of the decoder and is covered by
// TestInsertXMLElement_SelfClosingTargetOffsets.
//
//nolint:cyclop // Linear structural walk with per-token checks
func locateXMLElement(content string, segments []string) (int64, error) {
	dec := xml.NewDecoder(strings.NewReader(content))

	var stack []xmlFrame
	for {
		// InputOffset before reading a token is the start of that token and
		// the end of the previously returned one.
		closeStart := dec.InputOffset()
		tok, err := dec.Token()
		closeEnd := dec.InputOffset()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, fmt.Errorf("failed to parse XML: %w", err)
		}

		switch e := tok.(type) {
		case xml.StartElement:
			frame := xmlFrame{name: e.Name}
			if len(stack) > 0 {
				frame.ns = copyXMLNS(stack[len(stack)-1].ns)
			} else {
				frame.ns = make(map[string]string)
			}
			for _, a := range e.Attr {
				switch {
				case a.Name.Space == "xmlns":
					frame.ns[a.Name.Local] = a.Value
				case a.Name.Space == "" && a.Name.Local == "xmlns":
					frame.ns[""] = a.Value
				}
			}
			if len(stack) < len(segments) &&
				(len(stack) == 0 || stack[len(stack)-1].matched) {
				frame.matched = segmentMatches(segments[len(stack)], frame.ns, e.Name)
			}
			stack = append(stack, frame)

		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			if top.matched && len(stack) == len(segments) {
				// A synthesized EndElement (from a self-closing tag) does not
				// consume input, so its span is zero-width. An empty element
				// such as <a></a> also has closeStart == openEnd, but its end
				// tag does advance the decoder.
				if closeStart == closeEnd {
					return 0, fmt.Errorf("cannot insert into self-closing element <%s>", top.name.Local)
				}
				return closeStart, nil
			}
			stack = stack[:len(stack)-1]
		}
	}

	return 0, fmt.Errorf("target element path /%s not found", strings.Join(segments, "/"))
}

// segmentMatches reports whether a path segment matches an element name under
// the scoped namespace environment (see locateXMLElement for the contract).
func segmentMatches(segment string, ns map[string]string, name xml.Name) bool {
	local := segment
	space := ns[""]
	if idx := strings.IndexByte(segment, ':'); idx >= 0 {
		prefix := segment[:idx]
		local = segment[idx+1:]

		// An undeclared prefix must never fall back to the default or empty
		// namespace; only explicit declarations resolve.
		resolved, ok := ns[prefix]
		if !ok {
			return false
		}
		space = resolved
	}
	return name.Local == local && name.Space == space
}

func copyXMLNS(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// insertXMLElement finds the target element path and inserts the XML content within it.
// For example, with path "/project/build/plugins" and existing file, it locates the
// <plugins> element structurally via the token stream and splices the new XML fragment
// into the original source just before </plugins>.
func insertXMLElement(fileContent, elementPath, xmlToInsert string) (string, error) {
	segments, err := parseElementPath(elementPath)
	if err != nil {
		return "", err
	}

	insertAt, err := locateXMLElement(fileContent, segments)
	if err != nil {
		return "", err
	}
	// Validate the calculated offset before slicing so malformed or unexpected
	// input can never cause an out-of-range slice.
	if insertAt < 0 || insertAt > int64(len(fileContent)) {
		return "", fmt.Errorf("invalid insertion offset %d for path %s", insertAt, elementPath)
	}

	// Splice the new content before the closing tag of the target element.
	before := fileContent[:insertAt]
	after := fileContent[insertAt:]

	// Add proper indentation to the inserted content
	insertedLines := strings.Split(strings.TrimSpace(xmlToInsert), "\n")
	var result strings.Builder
	result.WriteString(before)
	result.WriteString("\n    ")
	for i, line := range insertedLines {
		result.WriteString(strings.TrimSpace(line))
		if i < len(insertedLines)-1 {
			result.WriteString("\n    ")
		}
	}
	result.WriteString("\n")
	result.WriteString(after)

	return result.String(), nil
}
