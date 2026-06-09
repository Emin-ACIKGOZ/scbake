// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package schema

import (
	"embed"
	"strings"
	"testing"
)

//go:embed testdata/valid_schema.json testdata/empty_schema.json
var testFS embed.FS

func TestParseSchemaBytes_Valid(t *testing.T) {
	data := []byte(`{
		"description": "A test schema",
		"variables": {
			"name": {
				"type": "string",
				"required": true,
				"description": "Project name"
			},
			"count": {
				"type": "number",
				"required": false,
				"default": "1",
				"description": "Number of things"
			}
		}
	}`)
	s, err := ParseSchemaBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Description != "A test schema" {
		t.Errorf("expected description %q, got %q", "A test schema", s.Description)
	}
	if len(s.Variables) != 2 {
		t.Fatalf("expected 2 variables, got %d", len(s.Variables))
	}
	v, ok := s.Variables["name"]
	if !ok {
		t.Fatal("expected variable 'name'")
	}
	if v.Type != TypeString {
		t.Errorf("expected type %q, got %q", TypeString, v.Type)
	}
	if !v.Required {
		t.Error("expected name to be required")
	}
}

func TestParseSchemaBytes_EmptyVariables(t *testing.T) {
	data := []byte(`{"variables": {}}`)
	_, err := ParseSchemaBytes(data)
	if err == nil {
		t.Fatal("expected error for empty variables")
	}
}

func TestParseSchemaBytes_MissingVariables(t *testing.T) {
	data := []byte(`{"description": "no vars"}`)
	_, err := ParseSchemaBytes(data)
	if err == nil {
		t.Fatal("expected error for missing variables")
	}
}

func TestParseSchemaBytes_InvalidJSON(t *testing.T) {
	_, err := ParseSchemaBytes([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReadSchema_Valid(t *testing.T) {
	s, err := ReadSchema(testFS, "testdata/valid_schema.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Variables) != 1 {
		t.Errorf("expected 1 variable, got %d", len(s.Variables))
	}
}

func TestReadSchema_NotFound(t *testing.T) {
	_, err := ReadSchema(testFS, "nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseSchema_Valid(t *testing.T) {
	r := strings.NewReader(`{
		"variables": {
			"x": { "type": "string", "required": true }
		}
	}`)
	s, err := ParseSchema(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := s.Variables["x"]; !ok {
		t.Error("expected variable 'x'")
	}
}

func TestParseSchema_EmptyVariables(t *testing.T) {
	r := strings.NewReader(`{"variables": {}}`)
	_, err := ParseSchema(r)
	if err == nil {
		t.Fatal("expected error for empty variables")
	}
}
