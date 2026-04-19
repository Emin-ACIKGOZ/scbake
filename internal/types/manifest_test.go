// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: gpl3-or-later

package types

import (
	"testing"
)

// TestManifestDeepCopy verifies that DeepCopy creates independent copies.
func TestManifestDeepCopy(t *testing.T) {
	original := &Manifest{
		SbakeVersion: "v0.0.1",
		Projects: []Project{
			{
				Name:      "backend",
				Path:      "backend",
				Language:  "go",
				Templates: []string{"git", "makefile"},
			},
		},
		Templates: []Template{
			{
				Name: "ci_github",
				Path: ".",
			},
		},
	}

	// Deep copy the manifest
	copied := original.DeepCopy()

	// Verify initial values match
	if copied.SbakeVersion != original.SbakeVersion {
		t.Errorf("SbakeVersion mismatch: %q != %q", copied.SbakeVersion, original.SbakeVersion)
	}

	if len(copied.Projects) != len(original.Projects) {
		t.Errorf("Projects length mismatch: %d != %d", len(copied.Projects), len(original.Projects))
	}

	if len(copied.Templates) != len(original.Templates) {
		t.Errorf("Templates length mismatch: %d != %d", len(copied.Templates), len(original.Templates))
	}

	// Modify the copy
	copied.SbakeVersion = "v0.0.2"
	if copied.Projects[0].Name != "backend" {
		t.Fatalf("Failed to copy project name")
	}
	copied.Projects[0].Name = "modified"
	copied.Projects[0].Templates[0] = "modified-template"

	copied.Templates[0].Name = "modified"

	// Verify original is unchanged
	if original.SbakeVersion != "v0.0.1" {
		t.Errorf("Original SbakeVersion was modified: %q", original.SbakeVersion)
	}

	if original.Projects[0].Name != "backend" {
		t.Errorf("Original project name was modified: %q", original.Projects[0].Name)
	}

	if original.Projects[0].Templates[0] != "git" {
		t.Errorf("Original template was modified: %q", original.Projects[0].Templates[0])
	}

	if original.Templates[0].Name != "ci_github" {
		t.Errorf("Original template name was modified: %q", original.Templates[0].Name)
	}
}

// TestManifestDeepCopyNil verifies that DeepCopy handles nil pointers gracefully.
func TestManifestDeepCopyNil(t *testing.T) {
	var m *Manifest
	copied := m.DeepCopy()
	if copied != nil {
		t.Error("DeepCopy of nil should return nil")
	}
}

// TestManifestDeepCopyEmpty verifies that DeepCopy handles empty manifests.
func TestManifestDeepCopyEmpty(t *testing.T) {
	original := &Manifest{
		SbakeVersion: "v0.0.1",
		Projects:     []Project{},
		Templates:    []Template{},
	}

	copied := original.DeepCopy()

	if copied.SbakeVersion != original.SbakeVersion {
		t.Errorf("SbakeVersion mismatch")
	}

	if len(copied.Projects) != 0 {
		t.Errorf("Expected 0 projects, got %d", len(copied.Projects))
	}

	if len(copied.Templates) != 0 {
		t.Errorf("Expected 0 templates, got %d", len(copied.Templates))
	}
}
