// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package schema

import (
	"testing"
)

func TestValidate_MissingRequired(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"service_id": {Type: TypeString, Required: true, Description: "Required service ID"},
		},
	}
	res := Validate(s, map[string]string{})
	if !res.HasErrors() {
		t.Fatal("expected validation errors")
	}
	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(res.Errors))
	}
	if res.Errors[0].VariableName != "service_id" {
		t.Errorf("expected error for 'service_id', got %q", res.Errors[0].VariableName)
	}
}

func TestValidate_AllProvided(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"name": {Type: TypeString, Required: true},
		},
	}
	res := Validate(s, map[string]string{"name": "myapp"})
	if res.HasErrors() {
		t.Errorf("unexpected errors: %v", res.Errors)
	}
}

func TestValidate_OptionalWithDefault(t *testing.T) {
	def := "go"
	s := &Schema{
		Variables: map[string]VariableDef{
			"build_tool": {Type: TypeString, Required: false, Default: &def},
		},
	}
	meta := map[string]string{}
	res := Validate(s, meta)
	if res.HasErrors() {
		t.Errorf("unexpected errors: %v", res.Errors)
	}
	if meta["build_tool"] != "go" {
		t.Errorf("expected default 'go', got %q", meta["build_tool"])
	}
}

func TestValidate_OptionalWithoutDefault(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"team": {Type: TypeString, Required: false},
		},
	}
	res := Validate(s, map[string]string{})
	if res.HasErrors() {
		t.Errorf("unexpected errors for optional without default: %v", res.Errors)
	}
}

func TestValidate_TypeMismatchNumber(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"port": {Type: TypeNumber, Required: true},
		},
	}
	res := Validate(s, map[string]string{"port": "not-a-number"})
	if !res.HasErrors() {
		t.Fatal("expected type error")
	}
	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(res.Errors))
	}
}

func TestValidate_TypeMismatchBool(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"enabled": {Type: TypeBool, Required: true},
		},
	}
	res := Validate(s, map[string]string{"enabled": "yes"})
	if !res.HasErrors() {
		t.Fatal("expected type error")
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"a": {Type: TypeString, Required: true},
			"b": {Type: TypeString, Required: true},
			"c": {Type: TypeString, Required: false},
		},
	}
	res := Validate(s, map[string]string{})
	if !res.HasErrors() {
		t.Fatal("expected errors")
	}
	if len(res.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(res.Errors), res.Errors)
	}
}

func TestValidate_EmptyValueIsMissing(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"name": {Type: TypeString, Required: true},
		},
	}
	res := Validate(s, map[string]string{"name": ""})
	if !res.HasErrors() {
		t.Fatal("expected error for empty required value")
	}
}

func TestValidationResult_Error(t *testing.T) {
	r := &ValidationResult{}
	if r.Error() != "" {
		t.Errorf("expected empty, got %q", r.Error())
	}
	r.Errors = append(r.Errors, ValidationError{VariableName: "x", Message: "missing"})
	if r.Error() == "" {
		t.Fatal("expected non-empty error string")
	}
}

func TestValidationResult_HasErrors(t *testing.T) {
	r := &ValidationResult{}
	if r.HasErrors() {
		t.Error("expected no errors")
	}
	r.Errors = append(r.Errors, ValidationError{})
	if !r.HasErrors() {
		t.Error("expected HasErrors=true")
	}
}

func TestValidate_PatternMatch(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"service_id": {Type: TypeString, Required: true, Pattern: "^[a-z0-9-]+$"},
		},
	}
	res := Validate(s, map[string]string{"service_id": "my-service-42"})
	if res.HasErrors() {
		t.Errorf("unexpected errors: %v", res.Errors)
	}
}

func TestValidate_PatternMismatch(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"service_id": {Type: TypeString, Required: true, Pattern: "^[a-z0-9-]+$"},
		},
	}
	res := Validate(s, map[string]string{"service_id": "MY_SERVICE"})
	if !res.HasErrors() {
		t.Fatal("expected pattern error")
	}
	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(res.Errors))
	}
}

func TestValidate_EnumMatch(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"runner_os": {Type: TypeString, Required: true, Enum: []string{"ubuntu-latest", "macos-latest"}},
		},
	}
	res := Validate(s, map[string]string{"runner_os": "ubuntu-latest"})
	if res.HasErrors() {
		t.Errorf("unexpected errors: %v", res.Errors)
	}
}

func TestValidate_EnumMismatch(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"runner_os": {Type: TypeString, Required: true, Enum: []string{"ubuntu-latest", "macos-latest"}},
		},
	}
	res := Validate(s, map[string]string{"runner_os": "windows-2019"})
	if !res.HasErrors() {
		t.Fatal("expected enum error")
	}
}

func TestValidate_PatternAndEnumCombined(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"runner_os": {
				Type:    TypeString,
				Required: true,
				Enum:    []string{"ubuntu-latest", "macos-latest", "windows-latest"},
				Pattern: "^(ubuntu|macos|windows)-latest$",
			},
		},
	}
	res := Validate(s, map[string]string{"runner_os": "ubuntu-latest"})
	if res.HasErrors() {
		t.Errorf("unexpected errors: %v", res.Errors)
	}
}

func TestValidationError_Error(t *testing.T) {
	e := ValidationError{VariableName: "foo", Message: "bar"}
	expected := `variable "foo": bar`
	if e.Error() != expected {
		t.Errorf("expected %q, got %q", expected, e.Error())
	}
}
