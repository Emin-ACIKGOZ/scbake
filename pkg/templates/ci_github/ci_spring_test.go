// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cighub

import (
	"strings"
	"testing"

	"scbake/internal/types"
)

// runBlock returns the indented body lines of the `run: |` scalar belonging to
// the named workflow step. Locating the step first keeps the assertion honest:
// an unrelated earlier step with its own run block must not be mistaken for the
// step under test.
func runBlock(t *testing.T, workflow, stepName string) []string {
	t.Helper()

	lines := strings.Split(workflow, "\n")
	stepIdx, stepIndent, ok := findStep(lines, stepName)
	if !ok {
		t.Fatalf("workflow has no %q step:\n%s", stepName, workflow)
	}
	runIdx, runIndent, ok := findStepRun(lines, stepIdx, stepIndent)
	if !ok {
		t.Fatalf("step %q has no `run: |` block:\n%s", stepName, workflow)
	}
	return indentedBody(lines, runIdx, runIndent)
}

// findStep returns the line index and indentation of the `- name: <name>` list
// item whose name equals name.
func findStep(lines []string, name string) (int, int, bool) {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		rest, isStep := strings.CutPrefix(trimmed, "- name:")
		if !isStep || strings.TrimSpace(rest) != name {
			continue
		}
		return i, len(line) - len(strings.TrimLeft(line, " ")), true
	}
	return -1, 0, false
}

// findStepRun returns the line index and indentation of the first `run: |`
// scalar inside the step starting at stepIdx, stopping at the step's first
// sibling list item or any shallower line.
func findStepRun(lines []string, stepIdx, stepIndent int) (int, int, bool) {
	for i := stepIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent < stepIndent {
			return -1, 0, false
		}
		if indent == stepIndent && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			return -1, 0, false
		}
		if strings.HasSuffix(strings.TrimSpace(line), "run: |") {
			return i, indent, true
		}
	}
	return -1, 0, false
}

// indentedBody collects the lines after a `run: |` line that are indented more
// deeply than it, preserving interior blank lines.
func indentedBody(lines []string, runIdx, runIndent int) []string {
	var body []string
	for _, next := range lines[runIdx+1:] {
		if strings.TrimSpace(next) == "" {
			body = append(body, next)
			continue
		}
		if len(next)-len(strings.TrimLeft(next, " ")) <= runIndent {
			break
		}
		body = append(body, next)
	}
	return body
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

	body := runBlock(t, out, "Build and Test All Projects")
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

// TestRunBlock_TargetsNamedStep proves the helper selects the run block of the
// requested step, not the first `run: |` in the workflow. Otherwise an earlier
// unrelated step could make the Spring regression assertion vacuous or wrong.
func TestRunBlock_TargetsNamedStep(t *testing.T) {
	workflow := `jobs:
  build:
    steps:
      - name: Preflight
        run: |
          echo "preflight"
      - name: Build and Test All Projects
        shell: bash
        run: |
          echo "build"
          ./mvnw clean verify
      - name: Postflight
        run: |
          echo "postflight"
`
	body := runBlock(t, workflow, "Build and Test All Projects")
	if len(body) == 0 {
		t.Fatalf("helper returned an empty body:\n%s", workflow)
	}
	var got []string
	for _, line := range body {
		got = append(got, strings.TrimSpace(line))
	}
	want := []string{`echo "build"`, "./mvnw clean verify"}
	if len(got) != len(want) {
		t.Fatalf("body = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("body[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	for _, line := range got {
		if strings.Contains(line, "preflight") || strings.Contains(line, "postflight") {
			t.Errorf("body includes a sibling step's command: %q", line)
		}
	}
}
