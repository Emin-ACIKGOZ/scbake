// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package templates_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/types"
	golang "scbake/pkg/lang/go"
	"scbake/pkg/tasks"
	"scbake/pkg/templates"
)

// templateRenderCase pairs a built-in template handler with the plan it
// produces for the fixed manifest below.
type templateRenderCase struct {
	name string
	plan func(targetPath string) ([]types.Task, error)
}

// TestBuiltInTemplatesAreWhitespaceClean is the regression guard for issue
// #275: every built-in template must render a file that ends with exactly one
// newline and has no line with trailing spaces or tabs, and rendered Go must
// already be gofmt-clean. Post-render formatting is deliberately not applied,
// so a defective template byte is caught here rather than papered over.
func TestBuiltInTemplatesAreWhitespaceClean(t *testing.T) {
	manifest := &types.Manifest{
		Metadata: map[string]string{"runner_os": "windows-latest"},
		Projects: []types.Project{
			{Name: "api", Path: "services/api", Language: "go"},
			{Name: "web", Path: "apps/web", Language: "svelte"},
			{Name: "service", Path: "services/service", Language: "spring"},
		},
	}

	cases := []templateRenderCase{{
		name: "go_lang",
		plan: func(targetPath string) ([]types.Task, error) {
			return (&golang.Handler{}).GetTasks(targetPath, "", "", nil)
		},
	}}
	for _, name := range templates.ListTemplates() {
		handler, err := templates.GetHandler(name)
		if err != nil {
			t.Fatalf("GetHandler(%q): %v", name, err)
		}
		cases = append(cases, templateRenderCase{
			name: name,
			plan: func(targetPath string) ([]types.Task, error) {
				return handler.GetTasks(targetPath, "", "")
			},
		})
	}

	gofmtPath, err := exec.LookPath("gofmt")
	if err != nil {
		t.Fatalf("gofmt is required for this regression test: %v", err)
	}

	rendered := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := t.TempDir()
			plan, err := tc.plan(target)
			if err != nil {
				t.Fatalf("building plan: %v", err)
			}

			for _, task := range plan {
				create, ok := task.(*tasks.CreateTemplateTask)
				if !ok {
					continue
				}
				rendered++
				execCtx := types.TaskContext{
					Ctx:        context.Background(),
					TargetPath: target,
					Manifest:   manifest,
				}
				if err := create.Execute(execCtx); err != nil {
					t.Fatalf("rendering %s: %v", create.OutputPath, err)
				}

				outputPath := filepath.Join(target, filepath.FromSlash(create.OutputPath))
				data, err := os.ReadFile(outputPath) //nolint:gosec // test temp path
				if err != nil {
					t.Fatalf("reading rendered %s: %v", create.OutputPath, err)
				}
				assertRenderedTextClean(t, create.OutputPath, data)
				if strings.HasSuffix(create.OutputPath, ".go") {
					assertGofmtClean(t, gofmtPath, outputPath)
				}
			}
		})
	}
	if rendered == 0 {
		t.Fatal("no built-in templates were rendered")
	}
}

// assertGofmtClean requires a rendered Go file to be already gofmt-clean, so a
// template cannot rely on a post-render formatting step.
func assertGofmtClean(t *testing.T, gofmtPath, path string) {
	t.Helper()

	out, err := exec.Command(gofmtPath, "-l", path).CombinedOutput() //nolint:gosec // resolved tool and test temp path
	if err != nil {
		t.Fatalf("gofmt -l %s failed: %v\n%s", path, err, out)
	}
	if len(bytes.TrimSpace(out)) != 0 {
		t.Errorf("generated Go is not gofmt-clean: %s", out)
	}
}

// assertRenderedTextClean checks the byte-level contract shared by every
// rendered text file: exactly one trailing newline and no trailing whitespace
// on any line.
func assertRenderedTextClean(t *testing.T, name string, data []byte) {
	t.Helper()

	if len(data) == 0 {
		t.Errorf("%s: rendered file is empty", name)
		return
	}
	if data[len(data)-1] != '\n' {
		t.Errorf("%s: rendered file does not end with a newline", name)
	}
	if bytes.HasSuffix(data, []byte("\n\n")) {
		t.Errorf("%s: rendered file ends with more than one newline", name)
	}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			t.Errorf("%s:%d: trailing whitespace", name, i+1)
		}
	}
}
