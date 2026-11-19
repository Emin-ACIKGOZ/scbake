// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/types"
)

// TestSaveRejectsInvalidUTF8 is the Save-level invariant for #206: every string
// persisted in scbake.toml must be valid UTF-8, even if a future writer skips
// the plan-time checks.
func TestSaveRejectsInvalidUTF8(t *testing.T) {
	const bad = "\x82"
	tests := []struct {
		name string
		m    *types.Manifest
		want string
	}{
		{name: "version", m: &types.Manifest{SbakeVersion: bad}, want: "scbake_version"},
		{name: "project name", m: &types.Manifest{Projects: []types.Project{{Name: bad}}}, want: "projects[0].name"},
		{name: "project path", m: &types.Manifest{Projects: []types.Project{{Path: bad}}}, want: "projects[0].path"},
		{name: "project language", m: &types.Manifest{Projects: []types.Project{{Language: bad}}}, want: "projects[0].language"},
		{name: "project template", m: &types.Manifest{Projects: []types.Project{{Templates: []string{bad}}}}, want: "projects[0].templates[0]"},
		{name: "template name", m: &types.Manifest{Templates: []types.Template{{Name: bad, Path: "."}}}, want: "templates[0].name"},
		{name: "template path", m: &types.Manifest{Templates: []types.Template{{Name: "x", Path: bad}}}, want: "templates[0].path"},
		{name: "metadata key", m: &types.Manifest{Metadata: map[string]string{bad: "v"}}, want: "metadata key"},
		{name: "metadata value", m: &types.Manifest{Metadata: map[string]string{"k": bad}}, want: `metadata "k"`},
		{name: "managed key", m: &types.Manifest{ManagedFiles: map[string]string{bad: "hash"}}, want: "managed_files key"},
		{name: "managed value", m: &types.Manifest{ManagedFiles: map[string]string{"k": bad}}, want: `managed_files "k"`},
		{name: "accepted key", m: &types.Manifest{AcceptedFiles: map[string]string{bad: "hash"}}, want: "accepted_files key"},
		{name: "accepted value", m: &types.Manifest{AcceptedFiles: map[string]string{"k": bad}}, want: `accepted_files "k"`},
		{name: "unmanaged entry", m: &types.Manifest{UnmanagedFiles: []string{bad}}, want: "unmanaged_files[0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			err := Save(tt.m, root)
			if err == nil {
				t.Fatalf("Save accepted invalid UTF-8")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
			if _, statErr := os.Stat(filepath.Join(root, "scbake.toml")); !os.IsNotExist(statErr) {
				t.Errorf("rejected Save wrote a manifest (stat err: %v)", statErr)
			}
		})
	}
}

// TestSaveAllowsValidUnicode keeps legitimate non-ASCII metadata working.
func TestSaveAllowsValidUnicode(t *testing.T) {
	root := t.TempDir()
	m := &types.Manifest{
		SbakeVersion: DefaultSbakeVersion,
		Metadata:     map[string]string{"copyright_holder": "© 2026 Açıkgöz"},
	}
	if err := Save(m, root); err != nil {
		t.Fatalf("Save rejected valid UTF-8: %v", err)
	}
	loaded, _, err := Load(root)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Metadata["copyright_holder"] != m.Metadata["copyright_holder"] {
		t.Errorf("metadata round-trip = %q", loaded.Metadata["copyright_holder"])
	}
}
