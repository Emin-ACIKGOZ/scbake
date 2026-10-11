// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"strings"
	"testing"
)

// TestValidateSetKeys_DeclaredKeyAccepted verifies that a key declared by a
// selected template's schema is accepted.
func TestValidateSetKeys_DeclaredKeyAccepted(t *testing.T) {
	err := ValidateSetKeys([]string{"makefile"}, "", map[string]string{"build_tool": "npm"})
	if err != nil {
		t.Fatalf("expected declared key to be accepted, got: %v", err)
	}
}

// TestValidateSetKeys_LangDeclaredKeyAccepted verifies that a key declared by
// the selected language pack's schema is accepted.
func TestValidateSetKeys_LangDeclaredKeyAccepted(t *testing.T) {
	err := ValidateSetKeys(nil, "go", map[string]string{"go_version": "1.23"})
	if err != nil {
		t.Fatalf("expected lang-declared key to be accepted, got: %v", err)
	}
}

// TestValidateSetKeys_UndeclaredKeyRejected verifies that an undeclared key is
// rejected and named in the error message.
func TestValidateSetKeys_UndeclaredKeyRejected(t *testing.T) {
	err := ValidateSetKeys([]string{"makefile"}, "", map[string]string{"bogus_key": "1"})
	if err == nil {
		t.Fatal("expected undeclared key to be rejected")
	}
	if !strings.Contains(err.Error(), "bogus_key") {
		t.Errorf("error should mention the offending key, got: %v", err)
	}
	// The allowed keys should be advertised so the user can fix the call.
	if !strings.Contains(err.Error(), "build_tool") {
		t.Errorf("error should list allowed keys, got: %v", err)
	}
}

// TestValidateSetKeys_ComplianceKeysAccepted verifies the compliance schema
// declares the keys its handler consumes from manifest metadata.
func TestValidateSetKeys_ComplianceKeysAccepted(t *testing.T) {
	err := ValidateSetKeys([]string{"compliance"}, "", map[string]string{
		"license":          "MIT",
		"copyright_holder": "X",
	})
	if err != nil {
		t.Fatalf("expected compliance keys to be accepted, got: %v", err)
	}
}

// TestValidateSetKeys_ComplianceKeysRejectedWithoutSchema verifies that keys
// only declared by compliance are rejected when compliance is not selected.
func TestValidateSetKeys_ComplianceKeysRejectedWithoutSchema(t *testing.T) {
	err := ValidateSetKeys([]string{"makefile"}, "", map[string]string{"license": "MIT"})
	if err == nil {
		t.Fatal("expected license to be rejected when no selected schema declares it")
	}
	if !strings.Contains(err.Error(), "license") {
		t.Errorf("error should mention license, got: %v", err)
	}
}

// TestValidateSetKeys_NoDeclaredVars verifies the clear message produced when
// none of the selected handlers declare any variables.
func TestValidateSetKeys_NoDeclaredVars(t *testing.T) {
	// "community" does not implement SchemaProvider.
	err := ValidateSetKeys([]string{"community"}, "", map[string]string{"anything": "1"})
	if err == nil {
		t.Fatal("expected key to be rejected when no schema declares variables")
	}
	if !strings.Contains(err.Error(), "anything") {
		t.Errorf("error should mention the offending key, got: %v", err)
	}
	if !strings.Contains(err.Error(), "declares any variables") {
		t.Errorf("error should explain that no variables are declared, got: %v", err)
	}
}

// TestValidateSetKeys_EmptySetVarsAccepted verifies that no --set values is a
// no-op and never errors.
func TestValidateSetKeys_EmptySetVarsAccepted(t *testing.T) {
	if err := ValidateSetKeys(nil, "", nil); err != nil {
		t.Fatalf("expected empty set vars to be accepted, got: %v", err)
	}
	if err := ValidateSetKeys([]string{"makefile"}, "go", map[string]string{}); err != nil {
		t.Fatalf("expected empty set vars map to be accepted, got: %v", err)
	}
}
