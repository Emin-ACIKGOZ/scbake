// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// usesKeyRe matches a `uses:` key, optionally as a YAML list item, tolerating
// whitespace before the colon, and captures the value that follows the colon.
var usesKeyRe = regexp.MustCompile(`^\s*(?:-\s+)?uses\s*:\s*(.*)$`)

// pinnedActionRe matches a third-party action reference pinned to a
// full-length, lowercase 40-hex commit SHA: `<owner>/<repo>@<sha>`.
var pinnedActionRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40}$`)

// TestWorkflowActionsPinnedToSHA is a regression guard for issue #43
// (SCB-045): every third-party GitHub Action referenced from a workflow must
// be pinned to an immutable full-length commit SHA. Mutable references such as
// `actions/checkout@v7` or `actions/checkout@main` can be moved by the action
// owner (or an attacker who compromises the owner) and would silently execute
// different code on the next run, breaking supply-chain integrity.
//
// Local actions (`./...`) are allowed. `docker://`-style references are out of
// scope and ignored, as they are not GitHub Action repositories.
func TestWorkflowActionsPinnedToSHA(t *testing.T) {
	root := moduleRoot(t)
	dir := filepath.Join(root, ".github", "workflows")

	paths, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("%s: no workflow files found; the scanner is broken", dir)
	}

	total := 0
	for _, path := range paths {
		n, findings := scanWorkflowUses(t, path)
		total += n
		for _, finding := range findings {
			t.Error(finding)
		}
	}

	if total == 0 {
		t.Fatalf("%s: no `uses:` entries detected; the scanner is broken", dir)
	}
}

// scanWorkflowUses returns the number of `uses:` entries in a workflow file and
// the unpinned third-party references found, one message per offending line.
func scanWorkflowUses(t *testing.T, path string) (int, []string) {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // fixed path under the module root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	total := 0
	var findings []string
	for i, line := range strings.Split(string(data), "\n") {
		value, ok := usesValue(line)
		if !ok {
			continue
		}
		total++
		if !actionPinned(value) {
			findings = append(findings, fmt.Sprintf(
				"%s:%d: third-party action must be pinned to a full-length commit SHA (got %q)",
				path, i+1, value,
			))
		}
	}
	return total, findings
}

// usesValue extracts the value of a `uses:` entry, dropping a trailing
// `# comment` (e.g. `...@<sha> # v7`). ok is false for non-`uses:` lines.
func usesValue(line string) (value string, ok bool) {
	m := usesKeyRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	value = strings.TrimSpace(m[1])
	if idx := strings.Index(value, "#"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value, value != ""
}

// actionPinned reports whether value is an acceptable action reference: a local
// path, a `docker://` image (out of scope), or an `<owner>/<repo>@<40-hex-sha>`
// pin.
func actionPinned(value string) bool {
	if strings.HasPrefix(value, "docker://") || strings.HasPrefix(value, "./") {
		return true
	}
	return pinnedActionRe.MatchString(value)
}
