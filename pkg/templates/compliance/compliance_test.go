// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mitchellh/go-spdx"
)

func TestComplianceHandler_GetTasks(t *testing.T) {
	h := &Handler{}
	tasks, err := h.GetTasks(".", "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	// SECURITY.md, dependabot.yml, LICENSE, CODEOWNERS
	expectedCount := 4
	if len(tasks) != expectedCount {
		t.Errorf("Expected %d tasks, got %d", expectedCount, len(tasks))
	}
}

func TestLicenseTask_Execute(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		metadata map[string]string
		wantErr  bool
	}{
		{
			"Valid Metadata",
			map[string]string{"license": "MIT", "copyright_holder": "Test Holder"},
			false,
		},
		{
			"Missing Metadata",
			nil,
			true,
		},
		{
			"Missing License",
			map[string]string{"copyright_holder": "Test Holder"},
			true,
		},
		{
			"Unsupported License",
			map[string]string{"license": "INVALID", "copyright_holder": "Test Holder"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &types.Manifest{Metadata: tt.metadata}
			tc := types.TaskContext{
				Ctx:        context.Background(),
				TargetPath: tmpDir,
				Manifest:   m,
			}

			task := &LicenseTask{TaskPrio: 100}
			err := task.Execute(tc)

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				licensePath := filepath.Join(tmpDir, "LICENSE")
				if _, err := os.Stat(licensePath); err != nil {
					t.Errorf("LICENSE file not created")
				}
			}
		})
	}
}

func TestLicenseTask_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{Metadata: map[string]string{"license": "MIT", "copyright_holder": "DryRun Holder"}}
	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest:   m,
		DryRun:     true,
	}
	task := &LicenseTask{TaskPrio: 100}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() with dry-run failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "LICENSE")); !os.IsNotExist(err) {
		t.Error("Dry-run should not create LICENSE file")
	}
}

func TestLicenseTask_ConflictStrategyFail(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata:     map[string]string{"license": "MIT", "copyright_holder": "Fail Holder"},
		ManagedFiles: map[string]string{"LICENSE": "originalhash"},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         m,
		ConflictStrategy: "fail",
	}
	task := &LicenseTask{TaskPrio: 100}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute should succeed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "LICENSE"), []byte("DRIFTED CONTENT"), 0644); err != nil {
		t.Fatalf("Failed to drift file: %v", err)
	}

	if err := task.Execute(tc); err == nil {
		t.Error("Expected drift error with 'fail' strategy")
	}
}

func TestLicenseTask_ConflictStrategyOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata:     map[string]string{"license": "MIT", "copyright_holder": "Overwrite Holder"},
		ManagedFiles: map[string]string{"LICENSE": "originalhash"},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         m,
		ConflictStrategy: "overwrite",
	}
	task := &LicenseTask{TaskPrio: 100}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute should succeed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "LICENSE"), []byte("DRIFTED"), 0644); err != nil {
		t.Fatalf("Failed to drift file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Overwrite strategy should succeed: %v", err)
	}
}

func TestLicenseTask_ConflictStrategyArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata:     map[string]string{"license": "MIT", "copyright_holder": "Artifact Holder"},
		ManagedFiles: map[string]string{"LICENSE": "originalhash"},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         m,
		ConflictStrategy: "artifact",
	}
	task := &LicenseTask{TaskPrio: 100}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute should succeed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "LICENSE"), []byte("DRIFTED"), 0644); err != nil {
		t.Fatalf("Failed to drift file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Artifact strategy should succeed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "LICENSE.scbake-new")); os.IsNotExist(err) {
		t.Error("Artifact file was not created")
	}
}

func TestLicenseTask_ConflictStrategyKeepLocal(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata:     map[string]string{"license": "MIT", "copyright_holder": "KeepLocal Holder"},
		ManagedFiles: map[string]string{"LICENSE": "originalhash"},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         m,
		ConflictStrategy: "keep-local",
	}
	task := &LicenseTask{TaskPrio: 100}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute should succeed: %v", err)
	}

	drifterContent := []byte("DRIFTED KEEPLOCAL")
	if err := os.WriteFile(filepath.Join(tmpDir, "LICENSE"), drifterContent, 0600); err != nil {
		t.Fatalf("Failed to drift file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Keep-local should succeed: %v", err)
	}

	//nolint:gosec // Test temp directory
	content, _ := os.ReadFile(filepath.Join(tmpDir, "LICENSE"))
	if string(content) != string(drifterContent) {
		t.Errorf("Keep-local should preserve local content. Got %q, want %q", string(content), string(drifterContent))
	}
}

func TestLicenseTask_ManagedFilesTracking(t *testing.T) {
	tmpDir := t.TempDir()
	m := &types.Manifest{
		Metadata: map[string]string{"license": "MIT", "copyright_holder": "Tracking Holder"},
	}
	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest:   m,
	}
	task := &LicenseTask{TaskPrio: 100}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if _, ok := m.ManagedFiles["LICENSE"]; !ok {
		t.Error("ManagedFiles should track LICENSE after execution")
	}
}

func TestLicenseTaskSkipsFetchForCleanUnchangedLicense(t *testing.T) {
	root := t.TempDir()
	metadata := map[string]string{"license": "MIT", "copyright_holder": "Acme"}
	content := []byte(fillLicensePlaceholders("License for <copyright holder>, <year>\n", strconv.Itoa(time.Now().Year()), "Acme"))
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &types.Manifest{
		Metadata: metadata,
		ManagedFiles: map[string]string{
			"LICENSE": tasks.HashContent(content),
		},
		ManagedInputs: map[string]string{
			licenseInputStateKey: licenseInputHash("MIT", "Acme", time.Now().Year()),
		},
	}
	calls := 0
	previousFetcher := fetchSPDXLicense
	fetchSPDXLicense = func(string) (*spdx.LicenseInfo, error) {
		calls++
		return nil, errors.New("network must not be used for an unchanged clean license")
	}
	t.Cleanup(func() { fetchSPDXLicense = previousFetcher })

	err := (&LicenseTask{}).Execute(types.TaskContext{
		Ctx: context.Background(), TargetPath: root, Manifest: m, DryRun: true,
	})
	if err != nil {
		t.Fatalf("unchanged license should be reusable offline: %v", err)
	}
	if calls != 0 {
		t.Fatalf("SPDX fetch called %d times for unchanged clean license, want 0", calls)
	}
}

func TestLicenseTaskFetchesWhenResolvedInputsChange(t *testing.T) {
	root := t.TempDir()
	oldMetadata := map[string]string{"license": "MIT", "copyright_holder": "Acme"}
	content := []byte(fillLicensePlaceholders("MIT for <copyright holder>, <year>\n", strconv.Itoa(time.Now().Year()), "Acme"))
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &types.Manifest{
		Metadata: map[string]string{"license": "Apache-2.0", "copyright_holder": "Acme"},
		ManagedFiles: map[string]string{
			"LICENSE": tasks.HashContent(content),
		},
		ManagedInputs: map[string]string{
			licenseInputStateKey: licenseInputHash(oldMetadata["license"], oldMetadata["copyright_holder"], time.Now().Year()),
		},
	}
	calls := 0
	previousFetcher := fetchSPDXLicense
	fetchSPDXLicense = func(id string) (*spdx.LicenseInfo, error) {
		calls++
		if id != "Apache-2.0" {
			return nil, fmt.Errorf("unexpected SPDX id %q", id)
		}
		return &spdx.LicenseInfo{ID: id, Text: "Apache License for <copyright holder> <year>\n"}, nil
	}
	t.Cleanup(func() { fetchSPDXLicense = previousFetcher })

	if err := (&LicenseTask{}).Execute(types.TaskContext{
		Ctx: context.Background(), TargetPath: root, Manifest: m,
	}); err != nil {
		t.Fatalf("changed license input should regenerate LICENSE: %v", err)
	}
	if calls != 1 {
		t.Fatalf("SPDX fetch called %d times for changed input, want 1", calls)
	}
	updated, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "Apache License") {
		t.Fatalf("LICENSE was not regenerated from changed input: %q", updated)
	}
	if m.ManagedInputs[licenseInputStateKey] != licenseInputHash("Apache-2.0", "Acme", time.Now().Year()) {
		t.Fatal("managed input fingerprint was not updated")
	}
}

func TestLicenseTask_Metadata(t *testing.T) {
	task := &LicenseTask{TaskPrio: 150}
	if task.Description() != "Generate LICENSE file" {
		t.Errorf("Description() mismatch")
	}
	if task.Priority() != 150 {
		t.Errorf("Priority() mismatch")
	}
}

// renderDependabot renders the embedded dependabot.yml template for the given
// manifest and returns its contents.
func renderDependabot(t *testing.T, m *types.Manifest) string {
	t.Helper()

	h := &Handler{}
	plan, err := h.GetTasks(".", "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	var task *tasks.CreateTemplateTask
	for _, tk := range plan {
		ct, ok := tk.(*tasks.CreateTemplateTask)
		if ok && ct.OutputPath == ".github/dependabot.yml" {
			task = ct
			break
		}
	}
	if task == nil {
		t.Fatal("dependabot task not found in plan")
	}

	dir := t.TempDir()
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: dir, Manifest: m}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(dir, ".github", "dependabot.yml")) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read generated dependabot.yml: %v", err)
	}
	return string(out)
}

// dependabotEntry is one parsed item of the generated `updates` list.
type dependabotEntry struct {
	ecosystem string
	directory string
}

// parseDependabotEntries extracts the entries under `updates:` from a generated
// dependabot.yml. It intentionally uses line parsing instead of a YAML
// dependency and fails when the updates section is missing or empty.
func parseDependabotEntries(t *testing.T, out string) []dependabotEntry {
	t.Helper()

	entries := parseDependabotEntryLines(dependabotUpdateLines(t, out))
	if len(entries) == 0 {
		t.Fatalf("dependabot.yml has an empty `updates` list:\n%s", out)
	}
	return entries
}

// dependabotUpdateLines returns the lines following the top-level `updates:` key.
func dependabotUpdateLines(t *testing.T, out string) []string {
	t.Helper()

	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "updates:" {
			return lines[i+1:]
		}
	}
	t.Fatalf("dependabot.yml has no `updates:` section:\n%s", out)
	return nil
}

// parseDependabotEntryLines parses `- package-ecosystem:` items and their
// following `directory:` value.
func parseDependabotEntryLines(lines []string) []dependabotEntry {
	var entries []dependabotEntry
	var current *dependabotEntry
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "- package-ecosystem:"):
			if current != nil {
				entries = append(entries, *current)
			}
			current = &dependabotEntry{
				ecosystem: unquoteYAMLValue(strings.TrimSpace(strings.TrimPrefix(trimmed, "- package-ecosystem:"))),
			}
		case strings.HasPrefix(trimmed, "directory:") && current != nil:
			current.directory = unquoteYAMLValue(strings.TrimSpace(strings.TrimPrefix(trimmed, "directory:")))
		}
	}
	if current != nil {
		entries = append(entries, *current)
	}
	return entries
}

// unquoteYAMLValue strips the double quotes the template applies to scalars.
func unquoteYAMLValue(v string) string {
	return strings.Trim(v, `"`)
}

// countDependabotEcosystems groups entries by ecosystem and returns the
// github-actions subset separately.
func countDependabotEcosystems(entries []dependabotEntry) (map[string]int, []dependabotEntry) {
	counts := make(map[string]int, len(entries))
	var githubActions []dependabotEntry
	for _, entry := range entries {
		counts[entry.ecosystem]++
		if entry.ecosystem == "github-actions" {
			githubActions = append(githubActions, entry)
		}
	}
	return counts, githubActions
}

// assertDependabotUpdateCounts verifies per-ecosystem counts, the total number
// of entries, and the single github-actions entry.
func assertDependabotUpdateCounts(t *testing.T, out string, entries []dependabotEntry, want map[string]int) {
	t.Helper()

	counts, githubActions := countDependabotEcosystems(entries)

	wantTotal := 1 // github-actions is always present exactly once.
	for ecosystem, n := range want {
		if got := counts[ecosystem]; got != n {
			t.Errorf("%s entries = %d, want %d:\n%s", ecosystem, got, n, out)
		}
		wantTotal += n
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
}

// TestDependabot_EcosystemCoverage is a regression test for issue #153: the
// generated updates list must never be null/empty, must include one
// github-actions entry with `directory: "/"`, and must map languages to
// gomod/npm/maven as appropriate.
func TestDependabot_EcosystemCoverage(t *testing.T) {
	tests := []struct {
		name     string
		projects []types.Project
		want     map[string]int
	}{
		{
			name: "no projects",
			want: map[string]int{"gomod": 0, "npm": 0, "maven": 0},
		},
		{
			name:     "go",
			projects: []types.Project{{Name: "api", Path: "services/api", Language: "go"}},
			want:     map[string]int{"gomod": 1, "npm": 0, "maven": 0},
		},
		{
			name:     "svelte",
			projects: []types.Project{{Name: "web", Path: "web", Language: "svelte"}},
			want:     map[string]int{"gomod": 0, "npm": 1, "maven": 0},
		},
		{
			name:     "spring",
			projects: []types.Project{{Name: "service", Path: "service", Language: "spring"}},
			want:     map[string]int{"gomod": 0, "npm": 0, "maven": 1},
		},
		{
			name: "mixed",
			projects: []types.Project{
				{Name: "api", Path: "services/api", Language: "go"},
				{Name: "web", Path: "web", Language: "svelte"},
				{Name: "service", Path: "service", Language: "spring"},
			},
			want: map[string]int{"gomod": 1, "npm": 1, "maven": 1},
		},
		{
			// Regression for the review finding: these substrings belong to a
			// quoted directory value, not to the top-level updates key.
			name: "path containing updates null",
			projects: []types.Project{
				{Name: "api", Path: "services/updates: null", Language: "go"},
				{Name: "web", Path: "web/updates: []", Language: "svelte"},
			},
			want: map[string]int{"gomod": 1, "npm": 1, "maven": 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderDependabot(t, &types.Manifest{Projects: tt.projects})
			entries := parseDependabotEntries(t, out)
			assertDependabotUpdateCounts(t, out, entries, tt.want)
		})
	}
}

// TestDependabot_BenignProjectRendersUnchanged pins the benign rendering.
func TestDependabot_BenignProjectRendersUnchanged(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{{Name: "api", Path: "services/api", Language: "go"}},
	}
	out := renderDependabot(t, m)

	if !strings.Contains(out, `directory: "services/api"`) {
		t.Errorf("generated dependabot.yml missing quoted directory:\n%s", out)
	}
}

// TestDependabot_ProjectPathCannotInject is a regression guard for SCB-008: a
// project path containing a quote or newline must not break out of the YAML
// scalar.
func TestDependabot_ProjectPathCannotInject(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{{
			Name:     "evil",
			Path:     "services/x\"\ntouch /tmp/pwned",
			Language: "go",
		}},
	}
	out := renderDependabot(t, m)

	if strings.Contains(out, "\ntouch /tmp/pwned") {
		t.Errorf("project path injected a new YAML line:\n%s", out)
	}
	if strings.Contains(out, `directory: "services/x"`) {
		t.Errorf("project path broke out of the YAML scalar:\n%s", out)
	}
}

// TestFillLicensePlaceholders is a regression test for issue #114.
func TestFillLicensePlaceholders(t *testing.T) {
	const year, holder = "2026", "Acme Corp"
	tests := []struct {
		name string
		text string
		want string
	}{
		{"MIT", "Copyright (c) <year> <copyright holders>", "Copyright (c) 2026 Acme Corp"},
		{"MIT-0", "Copyright <YEAR> <COPYRIGHT HOLDER>", "Copyright 2026 Acme Corp"},
		{"BSD-2-Clause-Patent", "Copyright (c) <YEAR> <COPYRIGHT HOLDERS>", "Copyright (c) 2026 Acme Corp"},
		{"UPL-1.0", "Copyright (c) [year] [copyright holders]", "Copyright (c) 2026 Acme Corp"},
		{"MulanPSL-2.0", "Copyright (c) [Year] [name of copyright holder]", "Copyright (c) 2026 Acme Corp"},
		{"unrelated", "<program> <software name> <owner organization> [Software Name]", "<program> <software name> <owner organization> [Software Name]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fillLicensePlaceholders(tc.text, year, holder); got != tc.want {
				t.Errorf("fillLicensePlaceholders() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFillLicensePlaceholdersPreservesDollar(t *testing.T) {
	got := fillLicensePlaceholders("Copyright (c) <year> <copyright holders>", "2026", "ACME $1")
	want := "Copyright (c) 2026 ACME $1"
	if got != want {
		t.Errorf("fillLicensePlaceholders() = %q, want %q", got, want)
	}
}
