// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package golinter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"strings"
	"testing"
	"time"
)

// resolveGolangciLint finds the linter used by the optional integration check.
// GOLANGCI_LINT overrides PATH lookup; the test suite never downloads it.
func resolveGolangciLint(t *testing.T) string {
	t.Helper()

	if override := os.Getenv("GOLANGCI_LINT"); override != "" {
		path, err := exec.LookPath(override)
		if err != nil {
			t.Fatalf("GOLANGCI_LINT=%q does not resolve to an executable: %v", override, err)
		}
		return path
	}

	path, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint not found; install it on PATH or set GOLANGCI_LINT")
	}
	return path
}

// renderGolangciConfig executes the task handler so the assertions run against
// the same rendered output that scbake writes into a project.
func renderGolangciConfig(t *testing.T) string {
	t.Helper()

	handler := &Handler{}
	plan, err := handler.GetTasks("", "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(plan) != 1 {
		t.Fatalf("expected exactly 1 task, got %d", len(plan))
	}

	task, ok := plan[0].(*tasks.CreateTemplateTask)
	if !ok {
		t.Fatalf("task type mismatch: got %T", plan[0])
	}

	dir := t.TempDir()
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: dir,
		Manifest:   &types.Manifest{},
	}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".golangci.yml")) //nolint:gosec // fixed name under t.TempDir
	if err != nil {
		t.Fatalf("read generated .golangci.yml: %v", err)
	}
	return string(content)
}

// meaningfulLines returns the non-empty, non-comment lines of the rendered
// config with surrounding whitespace removed.
func meaningfulLines(content string) []string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

// leadingWhitespace returns the indentation prefix of a line.
func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// topLevelBlock returns the meaningful lines nested under a top-level key.
func topLevelBlock(content, key string) []string {
	rawLines := strings.Split(content, "\n")
	keyIndent := ""
	found := false
	var block []string

	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if !found {
			if !strings.HasPrefix(trimmed, "#") && leadingWhitespace(line) == "" && strings.HasPrefix(trimmed, key+":") {
				keyIndent = leadingWhitespace(line)
				found = true
			}
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(leadingWhitespace(line)) <= len(keyIndent) {
			break
		}
		block = append(block, trimmed)
	}
	return block
}

// hasKeyLine reports whether the config contains key as a YAML mapping key,
// ignoring commented-out lines.
func hasKeyLine(content, key string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+":") {
			return true
		}
	}
	return false
}

// TestGolangciTemplate_V2VersionAndDefaults pins the v2 schema preamble and the
// opt-in linter policy that replaces the v1 disable-all key.
func TestGolangciTemplate_V2VersionAndDefaults(t *testing.T) {
	content := renderGolangciConfig(t)

	lines := meaningfulLines(content)
	if len(lines) == 0 {
		t.Fatal("rendered .golangci.yml is empty")
	}
	if got, want := lines[0], `version: "2"`; got != want {
		t.Errorf("first meaningful key = %q, want %q", got, want)
	}

	linters := topLevelBlock(content, "linters")
	foundDefault := false
	for _, line := range linters {
		if line == "default: none" {
			foundDefault = true
			break
		}
	}
	if !foundDefault {
		t.Errorf("linters.default: none not found in linters block: %v", linters)
	}
}

// TestGolangciTemplate_EnabledLinters ensures the v2 migration did not drop any
// of the linters the template previously enabled.
func TestGolangciTemplate_EnabledLinters(t *testing.T) {
	content := renderGolangciConfig(t)

	enabled := make(map[string]bool)
	inEnable := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "enable:" {
			inEnable = true
			continue
		}
		if !inEnable {
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			break
		}
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		if i := strings.Index(name, "#"); i >= 0 {
			name = strings.TrimSpace(name[:i])
		}
		enabled[name] = true
	}

	wantLinters := []string{
		"errcheck",
		"govet",
		"staticcheck",
		"unused",
		"bodyclose",
		"noctx",
		"revive",
		"whitespace",
		"ineffassign",
		"misspell",
		"gosec",
	}
	for _, name := range wantLinters {
		if !enabled[name] {
			t.Errorf("linter %q is not enabled; got enabled set %v", name, enabled)
		}
	}
}

// TestGolangciTemplate_NoV1Keys guards against reintroducing v1-only schema
// that makes `golangci-lint config verify` fail on v2.
func TestGolangciTemplate_NoV1Keys(t *testing.T) {
	content := renderGolangciConfig(t)

	if hasKeyLine(content, "Version") {
		t.Error("v1 key Version: must not be present (v2 uses lowercase version:)")
	}
	for _, key := range []string{"disable-all", "exclude-use-default"} {
		if strings.Contains(content, key) {
			t.Errorf("v1-only key %q must not be present", key)
		}
	}

	if hasKeyLine(content, "format") {
		t.Error("v1 key output.format must not be present (v2 uses output.formats)")
	}
	if !hasKeyLine(content, "formats") {
		t.Error("v2 key output.formats is missing")
	}
}

// TestGolangciTemplate_ConfigVerify runs the real v2 linter against the
// rendered file when the binary is available. CI skips this instead of
// downloading anything.
func TestGolangciTemplate_ConfigVerify(t *testing.T) {
	linter := resolveGolangciLint(t)
	t.Logf("using golangci-lint at %s", linter)

	content := renderGolangciConfig(t)

	dir := t.TempDir()
	configPath := filepath.Join(dir, ".golangci.yml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write rendered config to isolated temp dir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, linter, "config", "verify") //nolint:gosec // path resolved via GOLANGCI_LINT or PATH
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("golangci-lint config verify timed out after 60s\n%s", output)
	}
	if err != nil {
		t.Fatalf("golangci-lint config verify failed: %v\noutput:\n%s\nconfig:\n%s", err, output, content)
	}
	t.Logf("golangci-lint config verify passed: %s", strings.TrimSpace(string(output)))
}
