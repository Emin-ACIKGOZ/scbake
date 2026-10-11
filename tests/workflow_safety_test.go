// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// runKeyRe matches a `run:` key, optionally as a YAML list item and tolerating
// whitespace before the colon, capturing the value that follows the colon
// (which may be empty, a block-scalar indicator such as `|` / `>`, or an
// inline shell command).
var runKeyRe = regexp.MustCompile(`^\s*(?:-\s+)?run\s*:(\s*(.*))?$`)

// injectionRe matches an interpolation of an untrusted workflow input anywhere
// inside a `${{ ... }}` expression in a run: script. It covers both the full
// `github.event.inputs.*` context and the `inputs.*` shorthand available in
// workflow_dispatch, tolerates whitespace around the dots (GitHub expressions
// allow it), and matches when the context is wrapped in a function call or
// followed by bracket indexing.
var injectionRe = regexp.MustCompile(`\$\{\{[^}]*\b(?:github\s*\.\s*event\s*\.\s*inputs|inputs)\b`)

// blockScalarRe matches block-scalar indicators like `|`, `>-`, `|+`, `|2`.
var blockScalarRe = regexp.MustCompile(`^[|>][+-]?\d*$`)

// TestReleaseWorkflowNoInputInjection is a regression guard for issue #51
// (SCB-053): the workflow_dispatch `tag` input must never be interpolated
// directly into a `run:` shell script. It must be passed through `env:` and
// referenced as a shell variable, otherwise a crafted tag containing quotes,
// `$(...)`, or newlines can inject commands into a job holding
// `contents: write`.
func TestReleaseWorkflowNoInputInjection(t *testing.T) {
	root := moduleRoot(t)
	path := filepath.Join(root, ".github", "workflows", "release.yml")

	data, err := os.ReadFile(path) //nolint:gosec // fixed path under the module root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	content := string(data)

	if !strings.Contains(content, "TAG: ${{ github.event.inputs.tag }}") {
		t.Errorf("%s: expected the workflow_dispatch tag input to be passed via a `TAG:` env indirection; the fix may have been reverted", path)
	}

	blocks := collectRunBlocks(strings.Split(content, "\n"))
	if len(blocks) == 0 {
		t.Fatalf("%s: no run: scripts detected; the scanner is broken", path)
	}
	for _, block := range blocks {
		if m := injectionRe.FindString(block); m != "" {
			t.Errorf("%s: run: script must not interpolate an untrusted input directly (%q):\n%s", path, m, block)
		}
	}
}

// collectRunBlocks returns the body of every `run:` step in the given workflow
// lines. A block scalar (or bare `run:`) captures the following indented lines;
// an inline `run: <command>` contributes a single line.
func collectRunBlocks(lines []string) []string {
	var blocks []string
	for i := 0; i < len(lines); i++ {
		m := runKeyRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}

		runIndent := indentOf(lines[i])
		value := strings.TrimSpace(m[2])
		var body []string

		if value == "" || blockScalarRe.MatchString(value) {
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "" {
					body = append(body, "")
					continue
				}
				if indentOf(lines[j]) <= runIndent {
					break
				}
				body = append(body, lines[j])
				i = j
			}
		} else {
			body = append(body, value)
		}

		blocks = append(blocks, strings.Join(body, "\n"))
	}
	return blocks
}

// indentOf returns the number of leading spaces of a line. Tabs count as one
// column, which is sufficient for the space-indented workflows in this repo.
func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// moduleRoot walks up from the test file to the directory containing go.mod,
// falling back to `go env GOMOD` when source location is unavailable.
func moduleRoot(t *testing.T) string {
	t.Helper()

	if _, file, _, ok := runtime.Caller(0); ok {
		dir := filepath.Dir(file)
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	out, err := exec.CommandContext(context.Background(), "go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("locate module root: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatalf("locate module root: go env GOMOD returned %q", gomod)
	}
	return filepath.Dir(gomod)
}
