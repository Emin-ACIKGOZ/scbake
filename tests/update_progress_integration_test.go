// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"regexp"
	"strings"
	"testing"
)

// updateProgressLine matches a reporter milestone such as "[2/5] 📝 ...".
var updateProgressLine = regexp.MustCompile(`^\[(\d+)/(\d+)\] (.*)$`)

// TestUpdateProgressGolden is a regression guard for issue #271: `update` used
// to load the manifest twice, printing two "Loading manifest" steps and ending
// with [6/5] even though the reporter was sized for five milestones.
func TestUpdateProgressGolden(t *testing.T) {
	_, overrideDir := setupUpdateProject(t, "alpha\nshared\nomega\n")

	out, err := runCLI("update", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}

	var got []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if updateProgressLine.MatchString(line) {
			got = append(got, line)
		}
	}

	want := []string{
		"[1/5] 📖 Loading manifest (scbake.toml)...",
		"[2/5] 📝 Building execution plan...",
		"[3/5] 🚀 Executing plan...",
		"[4/5] ✍️ Updating manifest...",
		"[5/5] ✅ Committing transaction...",
	}
	if len(got) != len(want) {
		t.Fatalf("update printed %d progress steps, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %q, want %q", i+1, got[i], want[i])
		}
	}

	loads := 0
	for _, line := range got {
		if strings.Contains(line, "Loading manifest") {
			loads++
		}
	}
	if loads != 1 {
		t.Errorf("update printed %d Loading manifest steps, want 1:\n%s", loads, out)
	}
}
