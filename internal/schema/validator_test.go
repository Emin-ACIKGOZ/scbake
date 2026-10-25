// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"strings"
	"testing"
)

func stringPtr(s string) *string { return &s }

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
				Type:     TypeString,
				Required: true,
				Enum:     []string{"ubuntu-latest", "macos-latest", "windows-latest"},
				Pattern:  "^(ubuntu|macos|windows)-latest$",
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

func TestValidate_InvalidDefault(t *testing.T) {
	tests := []struct {
		name        string
		def         VariableDef
		wantMessage string
	}{
		{
			name:        "number type",
			def:         VariableDef{Type: TypeNumber, Default: stringPtr("not-a-number")},
			wantMessage: `invalid default "not-a-number": expected a number`,
		},
		{
			name:        "non-integer number",
			def:         VariableDef{Type: TypeNumber, Default: stringPtr("1.5")},
			wantMessage: `invalid default "1.5": expected a number`,
		},
		{
			name:        "empty default for number",
			def:         VariableDef{Type: TypeNumber, Default: stringPtr("")},
			wantMessage: `invalid default "": expected a number`,
		},
		{
			name:        "boolean type",
			def:         VariableDef{Type: TypeBool, Default: stringPtr("yes")},
			wantMessage: `invalid default "yes": expected true or false`,
		},
		{
			name:        "pattern mismatch",
			def:         VariableDef{Type: TypeString, Pattern: `^1\.\d+$`, Default: stringPtr("v1.22")},
			wantMessage: `invalid default "v1.22": does not match required pattern`,
		},
		{
			name:        "whitespace default",
			def:         VariableDef{Type: TypeString, Pattern: `^\S+$`, Default: stringPtr(" ")},
			wantMessage: `invalid default " ": does not match required pattern`,
		},
		{
			name:        "enum mismatch",
			def:         VariableDef{Type: TypeString, Enum: []string{"ubuntu-latest", "macos-latest"}, Default: stringPtr("windows-2019")},
			wantMessage: `invalid default "windows-2019": must be one of`,
		},
		{
			name:        "invalid pattern",
			def:         VariableDef{Type: TypeString, Pattern: "[", Default: stringPtr("x")},
			wantMessage: `invalid default "x": invalid pattern "["`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Schema{Variables: map[string]VariableDef{"v": tt.def}}
			meta := map[string]string{}
			res := Validate(s, meta)

			if !res.HasErrors() {
				t.Fatalf("expected validation error for invalid default")
			}
			if len(res.Errors) != 1 {
				t.Fatalf("expected 1 error, got %d: %v", len(res.Errors), res.Errors)
			}
			if res.Errors[0].VariableName != "v" {
				t.Errorf("expected error for 'v', got %q", res.Errors[0].VariableName)
			}
			if !strings.Contains(res.Errors[0].Message, tt.wantMessage) {
				t.Errorf("expected message containing %q, got %q", tt.wantMessage, res.Errors[0].Message)
			}
			if _, ok := meta["v"]; ok {
				t.Errorf("expected invalid default not to be applied, got %q", meta["v"])
			}
		})
	}
}

func TestValidate_ValidDefault(t *testing.T) {
	tests := []struct {
		name string
		def  VariableDef
		want string
	}{
		{
			name: "string",
			def:  VariableDef{Type: TypeString, Default: stringPtr("go")},
			want: "go",
		},
		{
			name: "number",
			def:  VariableDef{Type: TypeNumber, Default: stringPtr("8080")},
			want: "8080",
		},
		{
			name: "boolean",
			def:  VariableDef{Type: TypeBool, Default: stringPtr("true")},
			want: "true",
		},
		{
			name: "pattern match",
			def:  VariableDef{Type: TypeString, Pattern: `^1\.\d+$`, Default: stringPtr("1.22")},
			want: "1.22",
		},
		{
			name: "enum match",
			def:  VariableDef{Type: TypeString, Enum: []string{"ubuntu-latest", "macos-latest"}, Default: stringPtr("ubuntu-latest")},
			want: "ubuntu-latest",
		},
		{
			name: "empty string with no constraints",
			def:  VariableDef{Type: TypeString, Default: stringPtr("")},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Schema{Variables: map[string]VariableDef{"v": tt.def}}
			meta := map[string]string{}
			res := Validate(s, meta)

			if res.HasErrors() {
				t.Errorf("unexpected errors: %v", res.Errors)
			}
			if meta["v"] != tt.want {
				t.Errorf("expected default %q, got %q", tt.want, meta["v"])
			}
		})
	}
}

func TestValidate_InvalidDefaultWithEmptyProvidedValue(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"v": {Type: TypeNumber, Default: stringPtr("not-a-number")},
		},
	}
	meta := map[string]string{"v": ""}
	res := Validate(s, meta)

	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(res.Errors), res.Errors)
	}
	if res.Errors[0].VariableName != "v" {
		t.Errorf("expected error for 'v', got %q", res.Errors[0].VariableName)
	}
	if meta["v"] != "" {
		t.Errorf("expected existing empty value preserved, got %q", meta["v"])
	}
}

func TestValidate_InvalidDefaultLeavesOtherDefaultsApplied(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"a_valid":   {Type: TypeString, Default: stringPtr("go")},
			"z_invalid": {Type: TypeNumber, Default: stringPtr("nope")},
		},
	}
	meta := map[string]string{}
	res := Validate(s, meta)

	if !res.HasErrors() {
		t.Fatal("expected validation error for invalid default")
	}
	if meta["a_valid"] != "go" {
		t.Errorf("expected valid default 'go', got %q", meta["a_valid"])
	}
	if _, ok := meta["z_invalid"]; ok {
		t.Errorf("expected invalid default not to be applied, got %q", meta["z_invalid"])
	}
}

func TestValidate_InvalidDefaultReportsAllViolations(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"v": {Type: TypeString, Pattern: "^a", Enum: []string{"b"}, Default: stringPtr("zzz")},
		},
	}
	meta := map[string]string{}
	res := Validate(s, meta)

	if !res.HasErrors() {
		t.Fatal("expected validation errors for invalid default")
	}
	if len(res.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(res.Errors), res.Errors)
	}
	for _, err := range res.Errors {
		if err.VariableName != "v" {
			t.Errorf("expected error for 'v', got %q", err.VariableName)
		}
	}
	if !strings.Contains(res.Errors[0].Message, "does not match required pattern") {
		t.Errorf("expected pattern error, got %q", res.Errors[0].Message)
	}
	if !strings.Contains(res.Errors[1].Message, "must be one of") {
		t.Errorf("expected enum error, got %q", res.Errors[1].Message)
	}
	if _, ok := meta["v"]; ok {
		t.Errorf("expected invalid default not to be applied, got %q", meta["v"])
	}
}

func TestValidate_InvalidDefaultNilMetadata(t *testing.T) {
	s := &Schema{
		Variables: map[string]VariableDef{
			"v": {Type: TypeString, Pattern: "^a", Default: stringPtr("zzz")},
		},
	}
	res := Validate(s, nil)

	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(res.Errors), res.Errors)
	}
	if res.Errors[0].VariableName != "v" {
		t.Errorf("expected error for 'v', got %q", res.Errors[0].VariableName)
	}
}
