// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/ui"
	"scbake/pkg/tasks"
	"scbake/pkg/templates/compliance"
	"strings"
	"testing"

	"github.com/mitchellh/go-spdx"
)

// TestResolveUpdateSelection verifies I6: the manifest reproduces exactly the
// selection that `update` re-applies, scoped by normalized path so monorepo
// subprojects do not alias each other.
func TestResolveUpdateSelection(t *testing.T) {
	root := t.TempDir()
	m := &types.Manifest{
		Projects: []types.Project{
			{Name: "api", Path: "services/api", Language: "go"},
			{Name: "web", Path: "services/web", Language: "svelte"},
		},
		Templates: []types.Template{
			{Name: "makefile", Path: "services/api"},
			{Name: "makefile", Path: "services/api"}, // duplicate is deduplicated
			{Name: "editorconfig", Path: "."},
			{Name: "ci_github", Path: "services/api"},
		},
	}

	t.Run("subproject", func(t *testing.T) {
		lang, with, rel, err := resolveUpdateSelection(m, root, filepath.Join(root, "services", "api"))
		if err != nil {
			t.Fatalf("resolveUpdateSelection: %v", err)
		}
		if lang != "go" {
			t.Errorf("lang = %q, want go", lang)
		}
		if rel != "services/api" {
			t.Errorf("rel = %q, want services/api", rel)
		}
		if want := []string{"makefile", "ci_github"}; !reflect.DeepEqual(with, want) {
			t.Errorf("with = %v, want %v", with, want)
		}
	})

	t.Run("root", func(t *testing.T) {
		lang, with, rel, err := resolveUpdateSelection(m, root, root)
		if err != nil {
			t.Fatalf("resolveUpdateSelection: %v", err)
		}
		if lang != "" {
			t.Errorf("lang = %q, want empty", lang)
		}
		if rel != "." {
			t.Errorf("rel = %q, want .", rel)
		}
		if want := []string{"editorconfig"}; !reflect.DeepEqual(with, want) {
			t.Errorf("with = %v, want %v", with, want)
		}
	})
}

// TestManagedWriteTasks verifies that `update` executes only managed file
// writes and never re-runs side-effecting commands (F5).
func TestManagedWriteTasks(t *testing.T) {
	write := &tasks.CreateTemplateTask{OutputPath: "Makefile"}
	exec := &tasks.ExecCommandTask{Cmd: "go"}

	got := managedWriteTasks([]types.Task{exec, write})
	if len(got) != 1 {
		t.Fatalf("managedWriteTasks returned %d tasks, want 1", len(got))
	}
	if got[0] != types.Task(write) {
		t.Fatalf("managedWriteTasks kept the wrong task: %T", got[0])
	}
}

// TestManagedWriteTasks_KeepsLicenseTask is a regression test for #142: update
// and drift --repair dropped LicenseTask, so a deleted LICENSE was never restored.
func TestManagedWriteTasks_KeepsLicenseTask(t *testing.T) {
	license := &compliance.LicenseTask{TaskPrio: 100}
	exec := &tasks.ExecCommandTask{Cmd: "go"}

	got := managedWriteTasks([]types.Task{exec, license})
	if len(got) != 1 {
		t.Fatalf("managedWriteTasks returned %d tasks, want 1 (LicenseTask kept)", len(got))
	}
	if got[0] != types.Task(license) {
		t.Fatalf("managedWriteTasks kept the wrong task: %T", got[0])
	}
}

// fakeSPDX answers SPDX lookups from memory, so core tests never reach spdx.org.
// When fail is set, every request returns an error, as an offline machine would.
type fakeSPDX struct {
	calls int
	fail  bool
}

// RoundTrip implements http.RoundTripper.
func (f *fakeSPDX) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("network unavailable in test: " + req.URL.String())
	}
	id := strings.TrimSuffix(path.Base(req.URL.Path), ".json")
	body, err := json.Marshal(spdx.LicenseInfo{
		ID:   id,
		Text: id + " License\n\nCopyright <year> <copyright holder>\n",
	})
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

// installFakeSPDX routes spdx.DefaultClient through a fakeSPDX for the test and
// restores the real client afterwards.
func installFakeSPDX(t *testing.T) *fakeSPDX {
	t.Helper()
	f := &fakeSPDX{}
	previous := spdx.DefaultClient
	spdx.DefaultClient = &spdx.Client{HTTP: &http.Client{Transport: f}}
	t.Cleanup(func() { spdx.DefaultClient = previous })
	return f
}

// runManagedWrites executes the tasks that `update` keeps, the same way
// `update` does, against root with the given conflict strategy.
func runManagedWrites(t *testing.T, root string, m *types.Manifest, strategy string, plan []types.Task) error {
	t.Helper()
	tc := types.TaskContext{
		Ctx:              context.Background(),
		TargetPath:       root,
		Manifest:         m,
		ConflictStrategy: strategy,
	}
	reporter := ui.NewPlainReporter(1, false)
	return Execute(&types.Plan{Tasks: managedWriteTasks(plan)}, tc, reporter)
}

// TestUpdateRestoresDeletedLicense is the end-to-end regression for #142: after
// LICENSE is deleted, the update/repair path must select LicenseTask and recreate it.
func TestUpdateRestoresDeletedLicense(t *testing.T) {
	installFakeSPDX(t)
	root := t.TempDir()
	m := &types.Manifest{Metadata: map[string]string{"license": "MIT", "copyright_holder": "Acme"}}
	plan := []types.Task{
		&tasks.ExecCommandTask{Cmd: "go"},
		&compliance.LicenseTask{TaskPrio: 100},
	}
	// Create LICENSE directly, as a prior `apply` would, then delete it.
	if err := (&compliance.LicenseTask{TaskPrio: 100}).Execute(types.TaskContext{
		Ctx: context.Background(), TargetPath: root, Manifest: m,
	}); err != nil {
		t.Fatalf("initial LICENSE write failed: %v", err)
	}
	licensePath := filepath.Join(root, "LICENSE")
	if err := os.Remove(licensePath); err != nil {
		t.Fatal(err)
	}
	if err := runManagedWrites(t, root, m, types.ConflictFail, plan); err != nil {
		t.Fatalf("repair write failed: %v", err)
	}
	if _, err := os.Stat(licensePath); err != nil {
		t.Fatalf("LICENSE was not restored: %v", err)
	}
}

// TestUpdateLicenseHandEditFollowsConflictStrategy verifies that a hand-edited
// LICENSE is handled by the same conflict strategies as other managed files.
func TestUpdateLicenseHandEditFollowsConflictStrategy(t *testing.T) {
	installFakeSPDX(t)
	const edited = "my own license text\n"
	plan := []types.Task{&compliance.LicenseTask{TaskPrio: 100}}

	tests := []struct {
		strategy string
		wantErr  bool
		want     string // expected LICENSE content; empty means "not the edit"
	}{
		{types.ConflictFail, true, edited},
		{types.ConflictKeepLocal, false, edited},
		{types.ConflictOverwrite, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.strategy, func(t *testing.T) {
			root := t.TempDir()
			m := &types.Manifest{Metadata: map[string]string{"license": "MIT", "copyright_holder": "Acme"}}
			if err := runManagedWrites(t, root, m, types.ConflictFail, plan); err != nil {
				t.Fatalf("initial write failed: %v", err)
			}
			licensePath := filepath.Join(root, "LICENSE")
			if err := os.WriteFile(licensePath, []byte(edited), 0o600); err != nil {
				t.Fatal(err)
			}

			err := runManagedWrites(t, root, m, tt.strategy, plan)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			got, readErr := os.ReadFile(licensePath) // #nosec G304 -- test-controlled path
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tt.want != "" && string(got) != tt.want {
				t.Fatalf("LICENSE = %q, want %q", got, tt.want)
			}
			if tt.want == "" && string(got) == edited {
				t.Fatalf("LICENSE still holds the hand edit, want overwritten")
			}
		})
	}
}

// TestRunUpdateAppliesChangedLicense drives the real RunUpdate path (the one
// `update` and `drift --repair` use) with the default merge strategy: a changed
// metadata.license must be applied to the existing LICENSE.
func TestRunUpdateAppliesChangedLicense(t *testing.T) {
	installFakeSPDX(t)
	root := t.TempDir()
	m := &types.Manifest{
		Templates: []types.Template{{Name: "compliance", Path: "."}},
		Metadata:  map[string]string{"license": "MIT", "copyright_holder": "Acme"},
	}
	if err := manifest.Save(m, root); err != nil {
		t.Fatalf("manifest.Save: %v", err)
	}
	rc := RunContext{
		TargetPath:       root,
		ManifestPathArg:  ".",
		ConflictStrategy: types.ConflictMerge,
	}
	reporter := ui.NewPlainReporter(1, false)

	// First run writes LICENSE for MIT and records it in managed_files.
	if err := RunUpdate(rc, reporter); err != nil {
		t.Fatalf("initial RunUpdate: %v", err)
	}

	// RunUpdate rewrote scbake.toml, so reload it before editing (Save rejects a
	// stale copy), as a user editing the manifest would.
	m, _, err := manifest.LoadWithOptions(root, manifest.DiscoveryOptions{})
	if err != nil {
		t.Fatalf("manifest.LoadWithOptions: %v", err)
	}
	if m.ManagedInputs["compliance.LICENSE"] == "" {
		t.Fatal("RunUpdate did not persist the inputs used to generate LICENSE")
	}
	m.Metadata["license"] = "Apache-2.0"
	if err := manifest.Save(m, root); err != nil {
		t.Fatalf("manifest.Save: %v", err)
	}
	if err := RunUpdate(rc, reporter); err != nil {
		t.Fatalf("RunUpdate after license change: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "LICENSE")) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "Apache-2.0 License") {
		t.Fatalf("LICENSE was not updated to Apache-2.0; starts with %q", firstLine(string(got)))
	}
}

// TestUpdateAndRepairOfflineOnCleanComplianceProject is the offline check for
// #142: once a compliance project's LICENSE is recorded, update and drift
// --repair (both run RunUpdate) must succeed with no SPDX request, including
// under --dry-run. The network stub fails every request, so any fetch errors.
func TestUpdateAndRepairOfflineOnCleanComplianceProject(t *testing.T) {
	net := installFakeSPDX(t)
	root := t.TempDir()
	m := &types.Manifest{
		Templates: []types.Template{{Name: "compliance", Path: "."}},
		Metadata:  map[string]string{"license": "MIT", "copyright_holder": "Acme"},
	}
	if err := manifest.Save(m, root); err != nil {
		t.Fatalf("manifest.Save: %v", err)
	}
	rc := RunContext{TargetPath: root, ManifestPathArg: ".", ConflictStrategy: types.ConflictMerge}

	// Online first run generates LICENSE and records its managed_files and
	// managed_inputs, the state an offline run depends on.
	if err := RunUpdate(rc, ui.NewPlainReporter(1, false)); err != nil {
		t.Fatalf("online RunUpdate: %v", err)
	}
	if net.calls == 0 {
		t.Fatal("online RunUpdate did not use the SPDX client; stub is not in effect")
	}
	licensePath := filepath.Join(root, "LICENSE")
	before, err := os.ReadFile(licensePath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatal(err)
	}

	net.fail = true
	net.calls = 0
	for _, dryRun := range []bool{false, true} {
		rc.DryRun = dryRun
		if err := RunUpdate(rc, ui.NewPlainReporter(1, dryRun)); err != nil {
			t.Fatalf("offline RunUpdate (dry-run=%t) on clean project: %v", dryRun, err)
		}
	}
	if net.calls != 0 {
		t.Fatalf("clean offline runs made %d SPDX requests, want 0", net.calls)
	}
	after, err := os.ReadFile(licensePath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("clean LICENSE was modified offline")
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestApplyManifestChangesDeduplicates is a regression guard: the render-time
// manifest must not re-add a project or template that already exists, or
// templates ranging .Projects render every block twice after update.
func TestApplyManifestChangesDeduplicates(t *testing.T) {
	m := &types.Manifest{
		Projects:  []types.Project{{Name: "api", Path: "services/api", Language: "go"}},
		Templates: []types.Template{{Name: "makefile", Path: "."}},
	}
	changes := &manifestChanges{
		Projects: []types.Project{
			{Name: "api", Path: "services/api", Language: "go"}, // duplicate
			{Name: "web", Path: "services/web", Language: "svelte"},
		},
		Templates: []types.Template{
			{Name: "makefile", Path: "."}, // duplicate
			{Name: "ci_github", Path: "."},
		},
	}

	applyManifestChanges(m, changes)
	applyManifestChanges(m, changes) // a second run must not grow the manifest

	if len(m.Projects) != 2 {
		t.Errorf("projects = %d, want 2: %+v", len(m.Projects), m.Projects)
	}
	if len(m.Templates) != 2 {
		t.Errorf("templates = %d, want 2: %+v", len(m.Templates), m.Templates)
	}
}
