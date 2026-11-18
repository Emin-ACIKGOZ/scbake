// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cighub

import (
	"strings"
	"testing"

	"scbake/internal/types"
)

// runBlock returns the indented body lines of the first `run: |` scalar in the
// workflow, which is enough to assert that a shell command is part of the build
// step rather than merely present somewhere in the file.
func runBlock(t *testing.T, workflow string) []string {
	t.Helper()

	lines := strings.Split(workflow, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasSuffix(trimmed, "run: |") {
			continue
		}
		runIndent := len(line) - len(strings.TrimLeft(line, " "))

		var body []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				body = append(body, next)
				continue
			}
			indent := len(next) - len(strings.TrimLeft(next, " "))
			if indent <= runIndent {
				break
			}
			body = append(body, next)
		}
		return body
	}

	t.Fatalf("workflow has no `run: |` block:\n%s", workflow)
	return nil
}

// TestWorkflow_SpringProjectRunsTests is a regression guard for #225: the
// generated workflow advertised a "Build and Test" step but passed
// -DskipTests, so a failing DemoApplicationTests never failed CI.
func TestWorkflow_SpringProjectRunsTests(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{{Name: "demo", Path: "demo", Language: "spring"}},
	}
	out := renderWorkflow(t, m)

	if !strings.Contains(out, "./mvnw clean verify") {
		t.Errorf("generated workflow does not run the Maven verify lifecycle:\n%s", out)
	}
	if strings.Contains(out, "-DskipTests") {
		t.Errorf("generated workflow still skips tests:\n%s", out)
	}
	if !strings.Contains(out, "name: Build and Test All Projects") {
		t.Errorf("generated workflow lost the Build and Test step name:\n%s", out)
	}

	// The workflow must remain plausible YAML: no tabs, and the Spring command
	// must live inside the step's run block rather than at file scope.
	if strings.Contains(out, "\t") {
		t.Errorf("generated workflow contains a tab, which YAML forbids:\n%q", out)
	}

	body := runBlock(t, out)
	found := false
	for _, line := range body {
		if strings.TrimSpace(line) == "./mvnw clean verify" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("./mvnw clean verify is not inside the build step run block:\n%s", out)
	}
}
