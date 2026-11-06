// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package devcontainer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"scbake/internal/types"
	"strings"
	"testing"
	"text/template"
)

// fuzzTemplateFuncs mirrors the helpers registered by tasks.CreateTemplateTask.
// The devcontainer template currently uses none of them, but parsing with the
// same map keeps this target faithful to production rendering.
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

// fuzzProjects builds 0..4 projects by cycling the comma-like lists supplied by
// the fuzzer. Using lists lets a single seed exercise mixed languages.
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

// renderDevcontainerJSON renders the embedded devcontainer template in memory
// without touching the filesystem.
func renderDevcontainerJSONInMemory(m *types.Manifest) ([]byte, error) {
	tpl, err := template.New("devcontainer.json.tpl").
		Funcs(fuzzTemplateFuncs).
		ParseFS(templates, "devcontainer.json.tpl")
	if err != nil {
		return nil, fmt.Errorf("parse devcontainer.json.tpl: %w", err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, m); err != nil {
		return nil, fmt.Errorf("render devcontainer.json.tpl: %w", err)
	}
	return buf.Bytes(), nil
}

// FuzzDevcontainerJSON verifies that no project shape can break the rendered
// devcontainer.json. It is a regression fuzzer for issue #153, where a U+00A0
// non-breaking space made the generated file invalid JSON.
func FuzzDevcontainerJSON(f *testing.F) {
	f.Add(0, "", "", "")
	f.Add(1, "api", "services/api", "go")
	f.Add(1, "web", "web", "svelte")
	f.Add(1, "service", "service", "spring")
	f.Add(3, "api|web|service", "services/api|web|service", "go|svelte|spring")
	f.Add(1, "unnamed", "unnamed", "")
	f.Add(
		4,
		"quoted\"name|dot.name|slash/name|nbs\u00a0name",
		"services/\"quoted\"|a.b/c|dir/with space|nbs\u00a0path",
		"go|svelte|spring|",
	)

	f.Fuzz(func(t *testing.T, count int, names, paths, languages string) {
		m := &types.Manifest{Projects: fuzzProjects(count, names, paths, languages)}

		out, err := renderDevcontainerJSONInMemory(m)
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}

		if bytes.Contains(out, []byte("\u00a0")) {
			t.Fatalf("rendered devcontainer.json contains U+00A0 non-breaking space:\n%s", out)
		}

		if !json.Valid(out) {
			t.Fatalf("rendered devcontainer.json is not valid JSON:\n%s", out)
		}

		var doc map[string]any
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("rendered devcontainer.json does not unmarshal: %v\n%s", err, out)
		}
		if doc == nil {
			t.Fatalf("rendered devcontainer.json is not a JSON object:\n%s", out)
		}
	})
}
