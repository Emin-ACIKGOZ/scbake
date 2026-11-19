// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"strings"
	"testing"

	"scbake/internal/types"
)

// TestValidateManifestFieldsRejectsInvalidUTF8 is a regression test for #206:
// template paths and --set metadata are persisted in scbake.toml, so a raw
// invalid byte must be rejected before the manifest is written and made
// unloadable.
func TestValidateManifestFieldsRejectsInvalidUTF8(t *testing.T) {
	const bad = "\x82"
	tests := []struct {
		name string
		m    *types.Manifest
		want string
	}{
		{
			name: "template path",
			m:    &types.Manifest{Templates: []types.Template{{Name: "compliance", Path: bad}}},
			want: "template path",
		},
		{
			name: "template name",
			m:    &types.Manifest{Templates: []types.Template{{Name: bad, Path: "."}}},
			want: "template name",
		},
		{
			name: "metadata value",
			m:    &types.Manifest{Metadata: map[string]string{"copyright_holder": bad}},
			want: `metadata "copyright_holder"`,
		},
		{
			name: "metadata key",
			m:    &types.Manifest{Metadata: map[string]string{bad: "value"}},
			want: "metadata key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateManifestFields(tt.m)
			if err == nil {
				t.Fatalf("validateManifestFields accepted invalid UTF-8")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

// TestValidateManifestFieldsAllowsUnicode keeps legitimate non-ASCII metadata
// (a copyright holder, for example) working.
func TestValidateManifestFieldsAllowsUnicode(t *testing.T) {
	m := &types.Manifest{
		Templates: []types.Template{{Name: "compliance", Path: "services/api"}},
		Metadata:  map[string]string{"copyright_holder": "© 2026 Açıkgöz"},
	}
	if err := validateManifestFields(m); err != nil {
		t.Fatalf("validateManifestFields rejected valid UTF-8: %v", err)
	}
}
