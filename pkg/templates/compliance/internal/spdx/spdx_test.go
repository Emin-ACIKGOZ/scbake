// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package spdx

import (
	"strings"
	"testing"
)

func TestTextMIT(t *testing.T) {
	text, err := Text("MIT")
	if err != nil {
		t.Fatalf("Text(MIT) failed: %v", err)
	}
	if !strings.Contains(text, "MIT License") {
		t.Errorf("MIT text missing title:\n%s", text)
	}
	if !strings.Contains(text, "<year>") || !strings.Contains(text, "<copyright holders>") {
		t.Errorf("MIT text should retain SPDX placeholders:\n%s", text)
	}
}

func TestTextCaseInsensitive(t *testing.T) {
	upper, err := Text("MIT")
	if err != nil {
		t.Fatalf("Text(MIT) failed: %v", err)
	}

	lower, err := Text("mit")
	if err != nil {
		t.Fatalf("Text(mit) failed: %v", err)
	}

	if upper != lower {
		t.Error("license lookup should be case-insensitive")
	}
}

func TestTextUnknown(t *testing.T) {
	_, err := Text("NOT-A-REAL-LICENSE")
	if err == nil {
		t.Fatal("expected an error for an unknown SPDX license")
	}
	if !strings.Contains(err.Error(), "unknown SPDX license ID") {
		t.Errorf("error should name the unknown license, got: %v", err)
	}
}

func TestTextDeprecated(t *testing.T) {
	text, err := Text("GPL-2.0")
	if err != nil {
		t.Fatalf("Text(GPL-2.0) failed: %v", err)
	}
	if !strings.Contains(text, "GNU GENERAL PUBLIC LICENSE") {
		t.Errorf("deprecated GPL-2.0 text is missing or incomplete")
	}
}

func TestEveryEmbeddedLicenseResolves(t *testing.T) {
	for lower, canonical := range canonicalIDs {
		text, err := Text(lower)
		if err != nil {
			t.Fatalf("Text(%q) failed: %v", canonical, err)
		}
		if strings.TrimSpace(text) == "" {
			t.Fatalf("Text(%q) returned empty text", canonical)
		}
	}
}

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}
