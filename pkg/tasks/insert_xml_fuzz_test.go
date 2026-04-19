// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: gpl3-or-later

//go:build go1.18

package tasks

import (
	"strings"
	"testing"
)

// FuzzValidateXML tests XML validation with random inputs
func FuzzValidateXML(f *testing.F) {
	f.Add(`<root/>`)
	f.Add(`<root><child/></root>`)
	f.Add(`<root attr="value"><child/></root>`)
	f.Add(`<?xml version="1.0"?><root/>`)
	f.Add(`<root><unclosed>`)
	f.Add(`invalid xml`)

	f.Fuzz(func(_ *testing.T, xml string) {
		// Should not panic on any input
		err := validateXML(xml)
		// Error or not, we shouldn't panic
		_ = err
	})
}

// FuzzContainsNormalizedXML tests snippet detection with random inputs
func FuzzContainsNormalizedXML(f *testing.F) {
	f.Add(`<root><child/></root>`, `<child/>`)
	f.Add(`<a><b><c/></b></a>`, `<b><c/></b>`)
	f.Add(`line1
line2
line3`, `line2`)

	f.Fuzz(func(_ *testing.T, fileContent, snippet string) {
		// Should not panic on any input
		result := containsNormalizedXML(fileContent, snippet)
		// Verify result is boolean
		_ = result
	})
}

// FuzzInsertXMLElement tests insertion with random inputs
func FuzzInsertXMLElement(f *testing.F) {
	f.Add(`<root><target/></root>`, `/root/target`, `<new/>`)
	f.Add(`<a><b><c/></b></a>`, `/a/b`, `<d/>`)

	f.Fuzz(func(_ *testing.T, fileContent, path, insert string) {
		// Invalid paths should error, not panic
		_, _ = insertXMLElement(fileContent, path, insert)
	})
}

// TestContainsNormalizedXMLEdgeCases tests edge cases
func TestContainsNormalizedXMLEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		snippet  string
		expected bool
	}{
		{"empty snippet", "<root/>", "", false},
		{"empty file", "", "<root/>", false},
		{"both empty", "", "", false},
		{"exact match", "<root/>", "<root/>", true},
		{"with whitespace variance", "<root>\n  <child/>\n</root>", "<root>\n<child/>\n</root>", true},
		{"multiline snippet at start", "<root>\n<child/>\n</root>", "<root>\n<child/>", true},
		{"multiline snippet at end", "<root>\n<child/>\n</root>", "<child/>\n</root>", true},
		{"similar but not matching", "<root><child1/></root>", "<root><child2/></root>", false},
		{"snippet with extra whitespace", "  <plugin>  \n  <id>test</id>  \n  </plugin>  ", "<plugin>\n<id>test</id>\n</plugin>", true},
		{"single line repeated", "<a/>\n<a/>\n<a/>", "<a/>", true},
		{"unicode content", "<root>你好</root>", "<root>你好</root>", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsNormalizedXML(tt.file, tt.snippet)
			if result != tt.expected {
				t.Errorf("containsNormalizedXML() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestValidateXMLEdgeCases tests XML validation edge cases
func TestValidateXMLEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		xml     string
		wantErr bool
	}{
		{"empty string", "", false},  // XML decoder returns EOF on empty, not error
		{"whitespace only", "   \n  \t  ", false},  // Same - decoder returns EOF
		{"text only", "hello world", false},  // XML decoder returns EOF on text, not error
		{"valid simple", "<root/>", false},
		{"valid with namespace", "<root xmlns='http://example.com'/>", false},
		{"valid with declaration", "<?xml version='1.0'?><root/>", false},
		{"valid with cdata", "<root><![CDATA[content]]></root>", false},
		{"valid with comment", "<root><!-- comment --></root>", false},
		{"unclosed tag", "<root>", true},
		{"mismatched closing", "<root></other>", true},
		{"invalid attribute", "<root attr=>", true},
		{"valid complex", "<root><a><b><c/></b></a></root>", false},
		{"self-closing with attrs", "<root id='1' name='test'/>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateXML(tt.xml)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateXML() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestInsertXMLElementEdgeCases tests insertion edge cases
func TestInsertXMLElementEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		path    string
		insert  string
		wantErr bool
		check   func(string) bool
	}{
		{
			name:    "simple insertion",
			file:    "<root><target></target></root>",
			path:    "/root/target",
			insert:  "<new/>",
			wantErr: false,
			check:   func(r string) bool { return strings.Contains(r, "<new/>") },
		},
		{
			name:    "nested path",
			file:    "<a><b><c/></b></a>",
			path:    "/a/b",
			insert:  "<d/>",
			wantErr: false,
			check:   func(r string) bool { return strings.Contains(r, "<d/>") },
		},
		{
			name:    "path with attributes",
			file:    `<root><build id="1"><plugins></plugins></build></root>`,
			path:    "/root/build/plugins",
			insert:  "<plugin/>",
			wantErr: false,
			check:   func(r string) bool { return strings.Contains(r, "<plugin/>") },
		},
		{
			name:    "missing target element",
			file:    "<root/>",
			path:    "/root/missing",
			insert:  "<new/>",
			wantErr: true,
			check:   nil,
		},
		{
			name:    "invalid path",
			file:    "<root/>",
			path:    "invalid",
			insert:  "<new/>",
			wantErr: true,
			check:   nil,
		},
		{
			name:    "invalid insert XML",
			file:    "<root><target/></root>",
			path:    "/root/target",
			insert:  "<unclosed>",
			wantErr: true,
			check:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := insertXMLElement(tt.file, tt.path, tt.insert)
			if (err != nil) != tt.wantErr {
				t.Errorf("insertXMLElement() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				if !tt.check(result) {
					t.Errorf("insertXMLElement() result check failed: %s", result)
				}
			}
		})
	}
}
