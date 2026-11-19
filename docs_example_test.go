// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

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

// TestDocsGuidanceExamples pins the copy-paste contract for guidance that has
// drifted from the binary and release flow: `list` requires a subcommand, the
// Spring pack is Java, `--set` examples must use a built-in schema key, and the
// release footer must not pipe the installer into a shell.
func TestDocsGuidanceExamples(t *testing.T) {
	for _, tc := range []struct {
		file   string
		absent []string
	}{
		{"docs/INSTALLING.md", []string{"\nscbake list\n", "\nscbake list\r\n"}},
		{"docs/QUICK_START.md", []string{"Python (Spring"}},
		{".goreleaser.yaml", []string{"install.sh | bash"}},
		{"README.md", []string{"--set service_id=srv-abc"}},
	} {
		doc, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		text := strings.ReplaceAll(string(doc), "\r\n", "\n")
		for _, bad := range tc.absent {
			if strings.Contains(text, bad) {
				t.Errorf("%s contains stale guidance %q", tc.file, bad)
			}
		}
	}
}

// exampleRenderData mirrors the additive render-data wrapper supplied by
// pkg/tasks: embedding *types.Manifest keeps every legacy field available,
// while Project names the manifest project that owns the template's target.
type exampleRenderData struct {
	*types.Manifest
	Project *types.Project
}

// TestRustHandlerExampleTemplates keeps the documented Rust handler templates
// renderable. It executes them against a real manifest so an invalid pipeline
// (like the `index .Projects 0 | .Name` form fixed in issue #167) fails here
// instead of at `scbake new` time. The example templates use the project-scoped
// .Project field from issue #168, so the test supplies the same wrapper shape
// as pkg/tasks rather than the bare manifest.
func TestRustHandlerExampleTemplates(t *testing.T) {
	const templateDir = "docs/examples/rust-handler/templates"

	manifest := &types.Manifest{
		Projects: []types.Project{{
			Name:     "my-app",
			Path:     ".",
			Language: "rust",
		}},
	}
	projectName := manifest.Projects[0].Name
	renderData := exampleRenderData{Manifest: manifest, Project: &manifest.Projects[0]}

	for _, tc := range []struct {
		file string
		want string
	}{
		{"Cargo.toml.tpl", `name = "` + projectName + `"`},
		{"main.rs.tpl", "Hello from " + projectName},
	} {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(templateDir, tc.file)
			raw, err := os.ReadFile(path) //nolint:gosec // test reads fixed repository documentation paths
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}

			tpl, err := template.New(tc.file).Parse(string(raw))
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}

			var out strings.Builder
			if err := tpl.Execute(&out, renderData); err != nil {
				t.Fatalf("render %s: %v; the documented template must execute against a real manifest", path, err)
			}

			got := out.String()
			if !strings.Contains(got, projectName) {
				t.Errorf("rendered %s does not contain project name %q:\n%s", path, projectName, got)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("rendered %s does not contain %q:\n%s", path, tc.want, got)
			}
		})
	}
}
