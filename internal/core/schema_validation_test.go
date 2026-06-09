// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package core

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/templates"
	"testing"
)

//go:embed testdata/has_schema.json
var schemaHandlerFS embed.FS

// schemaHandler is a minimal handler that implements SchemaProvider for testing.
type schemaHandler struct{}

func (s *schemaHandler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	return nil, nil
}

func (s *schemaHandler) SchemaFS() embed.FS { return schemaHandlerFS }

func (s *schemaHandler) SchemaPath() string { return "testdata/has_schema.json" }

// noSchemaHandler does NOT implement SchemaProvider.
type noSchemaHandler struct{}

func (n *noSchemaHandler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	return nil, nil
}

// registerAndRestore registers a test handler and returns a cleanup function
// that restores the original handler (if any) for that name.
func registerAndRestore(name string, h templates.Handler) func() {
	templates.Register(name, h)
	return func() { templates.Register(name, &noSchemaHandler{}) }
}

//nolint:dupl // Test helpers are intentionally simple and duplicated for clarity.
func TestValidateSelectedTemplates_WithSchema(t *testing.T) {
	t.Cleanup(registerAndRestore("has-schema", &schemaHandler{}))

	rc := RunContext{
		WithFlag: []string{"has-schema"},
	}

	err := validateSelectedTemplates(rc, map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing required variable 'service_id'")
	}
}

//nolint:dupl // Test helpers are intentionally simple and duplicated for clarity.
func TestValidateSelectedTemplates_WithSchemaAndSet(t *testing.T) {
	t.Cleanup(registerAndRestore("has-schema", &schemaHandler{}))

	rc := RunContext{
		WithFlag: []string{"has-schema"},
	}

	err := validateSelectedTemplates(rc, map[string]string{"service_id": "srv-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSelectedTemplates_NoSchema(t *testing.T) {
	t.Cleanup(registerAndRestore("no-schema", &noSchemaHandler{}))

	rc := RunContext{
		WithFlag: []string{"no-schema"},
	}
	err := validateSelectedTemplates(rc, map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error for handler without schema: %v", err)
	}
}

func TestValidateSelectedTemplates_TemplateNotFound(t *testing.T) {
	rc := RunContext{
		WithFlag: []string{"nonexistent-template"},
	}
	err := validateSelectedTemplates(rc, map[string]string{})
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
}

func TestValidateSelectedTemplates_LangWithSchema(t *testing.T) {
	// The Go language handler implements SchemaProvider and requires go_version
	// to match pattern "^1\\.\\d+$". Set a valid value to verify the lang path.
	rc := RunContext{
		LangFlag: "go",
	}
	// go_version is optional with pattern, so no value should pass
	err := validateSelectedTemplates(rc, map[string]string{})
	if err != nil {
		t.Fatalf("optional var with pattern should pass when unset: %v", err)
	}

	// Setting a valid go_version should also pass
	err = validateSelectedTemplates(rc, map[string]string{"go_version": "1.23"})
	if err != nil {
		t.Fatalf("expected valid go_version to pass: %v", err)
	}
}

func TestValidateSelectedTemplates_LangWithSchemaInvalidPattern(t *testing.T) {
	rc := RunContext{
		LangFlag: "go",
	}
	err := validateSelectedTemplates(rc, map[string]string{"go_version": "v1.22"})
	if err == nil {
		t.Fatal("expected error for go_version not matching pattern ^1\\.\\d+$")
	}
}
