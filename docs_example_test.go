// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"

	"scbake/internal/manifest"
	"scbake/internal/types"

	"github.com/BurntSushi/toml"
)

// TestArchitectureManifestExample keeps the documented scbake.toml example
// decodable by the real manifest type. It parses the example instead of
// comparing bytes so formatting and hash values stay free to change, while
// renamed or undocumented tables fail.
func TestArchitectureManifestExample(t *testing.T) {
	const heading = "### 2. Manifest as Source of Truth"

	doc, err := os.ReadFile("docs/ARCHITECTURE.md")
	if err != nil {
		t.Fatalf("read docs/ARCHITECTURE.md: %v", err)
	}

	section := strings.ReplaceAll(string(doc), "\r\n", "\n")
	start := strings.Index(section, heading)
	if start < 0 {
		t.Fatalf("heading %q not found in docs/ARCHITECTURE.md", heading)
	}
	section = section[start+len(heading):]

	start = strings.Index(section, "```toml\n")
	if start < 0 {
		t.Fatalf("no toml example under %q", heading)
	}
	section = section[start+len("```toml\n"):]

	end := strings.Index(section, "```")
	if end < 0 {
		t.Fatal("unterminated toml example")
	}

	var m types.Manifest
	md, err := toml.Decode(section[:end], &m)
	if err != nil {
		t.Fatalf("documented example does not decode: %v", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		t.Errorf("documented example uses fields absent from types.Manifest: %v", undecoded)
	}
	if m.SbakeVersion != manifest.DefaultSbakeVersion {
		t.Errorf("scbake_version = %q, want %q", m.SbakeVersion, manifest.DefaultSbakeVersion)
	}
	for _, tc := range []struct {
		name    string
		present bool
	}{
		{"[[projects]] entry", len(m.Projects) > 0},
		{"[[templates]] entry", len(m.Templates) > 0},
		{"[metadata] table", len(m.Metadata) > 0},
		{"[managed_files] table", len(m.ManagedFiles) > 0},
	} {
		if !tc.present {
			t.Errorf("example should show at least one %s", tc.name)
		}
	}
}
