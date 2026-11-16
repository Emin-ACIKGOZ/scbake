// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIssue138_ReadTemplateConfiguredRegistryOverride pins that a configured
// registry still overrides the embedded template.
func TestIssue138_ReadTemplateConfiguredRegistryOverride(t *testing.T) {
	cacheDir := t.TempDir()
	pullIntoCache(t, cacheDir, "acme", "go-api", "testdata/simple.tpl", "from configured registry")

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "", cacheDir, "go-api", []string{"acme"})
	if err != nil {
		t.Fatalf("ReadTemplate: %v", err)
	}
	if string(content) != "from configured registry" {
		t.Errorf("content = %q, want the configured registry override", content)
	}
}

// TestIssue138_ReadTemplateUnconfiguredRegistryFallsBackToEmbedded pins the
// fail-closed allowlist for nil, empty, and non-matching registry names.
func TestIssue138_ReadTemplateUnconfiguredRegistryFallsBackToEmbedded(t *testing.T) {
	cacheDir := t.TempDir()
	pullIntoCache(t, cacheDir, "acme", "go-api", "testdata/simple.tpl", "from unconfigured registry")

	embedded, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "", "", "", nil)
	if err != nil {
		t.Fatalf("reading embedded baseline: %v", err)
	}

	for name, allow := range map[string][]string{
		"nil":        nil,
		"empty":      {},
		"other name": {"other"},
	} {
		t.Run(name, func(t *testing.T) {
			content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "", cacheDir, "go-api", allow)
			if err != nil {
				t.Fatalf("ReadTemplate: %v", err)
			}
			if string(content) != string(embedded) {
				t.Errorf("content = %q, want the embedded fallback %q", content, embedded)
			}
		})
	}
}

// TestIssue138_ResolveTemplateSourceTiers covers all three tiers and the
// no-source case, including the labels used in dry-run output.
func TestIssue138_ResolveTemplateSourceTiers(t *testing.T) {
	cacheDir := t.TempDir()
	pullIntoCache(t, cacheDir, "acme", "go-api", "testdata/simple.tpl", "from registry")

	overrideDir := t.TempDir()
	overridePath := filepath.Join(overrideDir, "testdata/simple.tpl")
	if err := os.MkdirAll(filepath.Dir(overridePath), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte("from override"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cases := []struct {
		name      string
		tplPath   string
		override  string
		allow     []string
		wantTier  string
		wantName  string
		wantLabel string
	}{
		{
			name: "override wins", tplPath: "testdata/simple.tpl", override: overrideDir,
			allow: []string{"acme"}, wantTier: SourceOverride, wantLabel: "template-dir",
		},
		{
			name: "configured registry", tplPath: "testdata/simple.tpl",
			allow: []string{"acme"}, wantTier: SourceRegistry, wantName: "acme", wantLabel: "registry: acme",
		},
		{
			name: "unconfigured registry falls back", tplPath: "testdata/simple.tpl",
			allow: []string{"other"}, wantTier: SourceEmbedded, wantLabel: "built-in",
		},
		{
			name: "nil allowlist falls back", tplPath: "testdata/simple.tpl",
			wantTier: SourceEmbedded, wantLabel: "built-in",
		},
		{
			name: "missing embedded path has no source", tplPath: "missing.tpl",
			wantTier: "", wantLabel: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := ResolveTemplateSource(testEmbedFS, tc.tplPath, tc.override, cacheDir, "go-api", tc.allow)
			if src.Tier != tc.wantTier {
				t.Errorf("Tier = %q, want %q", src.Tier, tc.wantTier)
			}
			if src.Registry != tc.wantName {
				t.Errorf("Registry = %q, want %q", src.Registry, tc.wantName)
			}
			if src.Label() != tc.wantLabel {
				t.Errorf("Label() = %q, want %q", src.Label(), tc.wantLabel)
			}
		})
	}
}
