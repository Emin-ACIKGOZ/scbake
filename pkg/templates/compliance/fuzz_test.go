// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"bytes"
	"fmt"
	"scbake/internal/types"
	"strings"
	"testing"
	"text/template"
)

// fuzzTemplateFuncs mirrors the helpers registered by tasks.CreateTemplateTask.
// Only quote is used by dependabot.yml.tpl, but parsing with the same helper
// keeps this target faithful to production rendering.
var fuzzTemplateFuncs = template.FuncMap{
	"quote": func(s string) string {
		return fmt.Sprintf("%q", s)
	},
}

// fuzzList splits the pipe-separated fields supplied to the fuzz target. Empty
// input still yields one empty value so every project gets a field.
func fuzzList(s string) []string {
	return strings.Split(s, "|")
}

// fuzzProjects builds 0..4 projects by cycling the lists supplied by the
// fuzzer. Using lists lets a single seed exercise mixed languages.
func fuzzProjects(count int, names, paths, languages string) []types.Project {
	count %= 5
	if count < 0 {
		count += 5
	}

	nameList := fuzzList(names)
	pathList := fuzzList(paths)
	langList := fuzzList(languages)

	projects := make([]types.Project, 0, count)
	for i := 0; i < count; i++ {
		projects = append(projects, types.Project{
			Name:     nameList[i%len(nameList)],
			Path:     pathList[i%len(pathList)],
			Language: langList[i%len(langList)],
		})
	}
	return projects
}

// renderDependabotInMemory renders the embedded dependabot template without
// touching the filesystem.
func renderDependabotInMemory(m *types.Manifest) (string, error) {
	tpl, err := template.New("dependabot.yml.tpl").
		Funcs(fuzzTemplateFuncs).
		ParseFS(templates, "templates/dependabot.yml.tpl")
	if err != nil {
		return "", fmt.Errorf("parse dependabot.yml.tpl: %w", err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, m); err != nil {
		return "", fmt.Errorf("render dependabot.yml.tpl: %w", err)
	}
	return buf.String(), nil
}

// fuzzDependabotEntry is one parsed item of the generated `updates` list.
type fuzzDependabotEntry struct {
	ecosystem string
	directory string
}

// fuzzParseDependabotEntries performs deterministic line-based parsing of the
// generated updates list, failing on a missing or empty updates section.
func fuzzParseDependabotEntries(t *testing.T, out string) []fuzzDependabotEntry {
	t.Helper()

	lines := strings.Split(out, "\n")
	updatesIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "updates:" {
			updatesIdx = i
			break
		}
	}
	if updatesIdx < 0 {
		t.Fatalf("dependabot.yml has no `updates:` section:\n%s", out)
	}

	var entries []fuzzDependabotEntry
	var current *fuzzDependabotEntry
	for _, line := range lines[updatesIdx+1:] {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "- package-ecosystem:"):
			if current != nil {
				entries = append(entries, *current)
			}
			current = &fuzzDependabotEntry{
				ecosystem: unquoteFuzzYAMLValue(strings.TrimSpace(strings.TrimPrefix(trimmed, "- package-ecosystem:"))),
			}
		case strings.HasPrefix(trimmed, "directory:") && current != nil:
			current.directory = unquoteFuzzYAMLValue(strings.TrimSpace(strings.TrimPrefix(trimmed, "directory:")))
		}
	}
	if current != nil {
		entries = append(entries, *current)
	}
	if len(entries) == 0 {
		t.Fatalf("dependabot.yml has an empty `updates` list:\n%s", out)
	}
	if !strings.Contains(out, "- package-ecosystem:") {
		t.Fatalf("dependabot.yml has no `- package-ecosystem:` line:\n%s", out)
	}
	return entries
}

// unquoteFuzzYAMLValue strips the double quotes the template applies to scalars.
func unquoteFuzzYAMLValue(v string) string {
	return strings.Trim(v, `"`)
}

// FuzzDependabotUpdates verifies the issue #153 invariants for every project
// shape: rendering never fails, updates is never null/empty, exactly one
// github-actions entry targets "/", and language ecosystems appear exactly when
// their language is declared.
func FuzzDependabotUpdates(f *testing.F) {
	f.Add(0, "", "", "")
	f.Add(1, "api", "services/api", "go")
	f.Add(1, "web", "web", "svelte")
	f.Add(1, "service", "service", "spring")
	f.Add(3, "api|web|service", "services/api|web|service", "go|svelte|spring")
	f.Add(1, "unnamed", "unnamed", "")
	f.Add(
		2,
		"api|web",
		"services/updates: null|web/updates: []",
		"go|svelte",
	)
	f.Add(
		4,
		"quoted\"name|dot.name|slash/name|nbs\u00a0name",
		"services/\"quoted\"|a.b/c|dir/with space|nbs\u00a0path",
		"go|svelte|spring|",
	)

	f.Fuzz(func(t *testing.T, count int, names, paths, languages string) {
		projects := fuzzProjects(count, names, paths, languages)
		m := &types.Manifest{Projects: projects}

		out, err := renderDependabotInMemory(m)
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}

		entries := fuzzParseDependabotEntries(t, out)

		expected := map[string]int{"gomod": 0, "npm": 0, "maven": 0}
		for _, p := range projects {
			switch p.Language {
			case "go":
				expected["gomod"]++
			case "svelte":
				expected["npm"]++
			case "spring":
				expected["maven"]++
			}
		}

		counts := make(map[string]int, len(entries))
		var githubActions []fuzzDependabotEntry
		for _, entry := range entries {
			counts[entry.ecosystem]++
			if entry.ecosystem == "github-actions" {
				githubActions = append(githubActions, entry)
			}
		}

		for ecosystem, want := range expected {
			if got := counts[ecosystem]; got != want {
				t.Errorf("%s entries = %d, want %d:\n%s", ecosystem, got, want, out)
			}
		}

		wantTotal := 1 // github-actions is always present exactly once.
		for _, want := range expected {
			wantTotal += want
		}
		if len(entries) != wantTotal {
			t.Errorf("total updates entries = %d, want %d:\n%s", len(entries), wantTotal, out)
		}

		if len(githubActions) != 1 {
			t.Fatalf("github-actions entries = %d, want exactly 1:\n%s", len(githubActions), out)
		}
		if githubActions[0].directory != "/" {
			t.Errorf("github-actions directory = %q, want %q:\n%s", githubActions[0].directory, "/", out)
		}
	})
}
