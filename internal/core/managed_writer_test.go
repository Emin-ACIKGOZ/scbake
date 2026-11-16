// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/drift"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/ui"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"scbake/pkg/templates/compliance"
	"strings"
	"testing"
)

// fakeManagedWriterTask declares itself a managed writer and writes through the
// same reconciliation path as the real tasks. It exists to prove the marker, not
// the concrete type, is what update keys on.
type fakeManagedWriterTask struct {
	outputPath string
	content    string
}

func (t *fakeManagedWriterTask) Description() string { return "fake managed writer" }
func (t *fakeManagedWriterTask) Priority() int       { return int(types.PrioConfigUniversal) }
func (t *fakeManagedWriterTask) WritesManagedFile() bool {
	return true
}

func (t *fakeManagedWriterTask) Execute(tc types.TaskContext) error {
	return tasks.WriteManagedFile(tc, t.outputPath, []byte(t.content), fileutil.FilePerms)
}

// writeLicenseFixture records a handcrafted compliance manifest whose LICENSE
// ledger entry matches content. The test owns the file creation so the repair
// scenario can delete a ledger-tracked file.
func writeLicenseFixture(t *testing.T, root, licenseID, content string) {
	t.Helper()

	m := &types.Manifest{
		SbakeVersion: manifest.DefaultSbakeVersion,
		Templates:    []types.Template{{Name: "compliance", Path: "."}},
		Metadata: map[string]string{
			"license":          licenseID,
			"copyright_holder": "Test Holder",
		},
		ManagedFiles: map[string]string{
			"LICENSE": tasks.HashContent([]byte(content)),
		},
	}
	if err := manifest.Save(m, root); err != nil {
		t.Fatalf("write fixture manifest: %v", err)
	}
}

// assertRenderedLicense checks observable properties of a rendered license:
// it names the expected license, substitutes the metadata holder, and leaves no
// unsubstituted year or holder placeholders.
func assertRenderedLicense(t *testing.T, text, title, holder string) {
	t.Helper()

	if !strings.Contains(text, title) {
		t.Errorf("LICENSE is missing %q:\n%s", title, text)
	}
	if !strings.Contains(text, holder) {
		t.Errorf("LICENSE is missing the copyright holder %q:\n%s", holder, text)
	}
	if strings.Contains(text, "<year>") || strings.Contains(text, "<copyright holders>") {
		t.Errorf("LICENSE still contains SPDX placeholders:\n%s", text)
	}
}

func licenseRunContext(root string) RunContext {
	return RunContext{
		TargetPath:      root,
		ManifestPathArg: ".",
		Ctx:             context.Background(),
	}
}

// TestManagedWriteTasks_MarkerKeepsManagedWriters is the regression for #142:
// update/repair must keep every task that declares itself a managed file writer,
// not just CreateTemplateTask. ExecCommandTask and untracked writers stay out.
func TestManagedWriteTasks_MarkerKeepsManagedWriters(t *testing.T) {
	fake := &fakeManagedWriterTask{outputPath: "managed.txt", content: "managed"}
	exec := &tasks.ExecCommandTask{Cmd: "go"}

	got := managedWriteTasks([]types.Task{exec, fake})
	if len(got) != 1 {
		t.Fatalf("managedWriteTasks returned %d tasks, want 1", len(got))
	}
	if got[0] != types.Task(fake) {
		t.Fatalf("managedWriteTasks kept the wrong task: %T", got[0])
	}

	h := &compliance.Handler{}
	planTasks, err := h.GetTasks(".", "", "")
	if err != nil {
		t.Fatalf("compliance.GetTasks: %v", err)
	}

	filtered := managedWriteTasks(planTasks)
	var licensed bool
	for _, task := range filtered {
		switch task.(type) {
		case *compliance.LicenseTask:
			licensed = true
		case *tasks.AppendFileTask:
			t.Errorf("managedWriteTasks kept untracked AppendFileTask")
		}
	}
	if !licensed {
		t.Fatalf("managedWriteTasks dropped *compliance.LicenseTask from %T", planTasks)
	}
}

// TestRunUpdateRepairsDeletedLicense deletes a ledger-tracked LICENSE and runs
// the repair path (drift --repair delegates to RunUpdate). The file must be
// restored and the next drift scan must be clean.
func TestRunUpdateRepairsDeletedLicense(t *testing.T) {
	root := t.TempDir()
	const content = "stale MIT license text\n"
	writeLicenseFixture(t, root, "MIT", content)

	licensePath := filepath.Join(root, "LICENSE")
	if err := os.WriteFile(licensePath, []byte(content), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(licensePath); err != nil {
		t.Fatalf("delete ledger-tracked LICENSE: %v", err)
	}

	reporter := ui.NewPlainReporter(5, false)
	if err := RunUpdate(licenseRunContext(root), reporter); err != nil {
		t.Fatalf("RunUpdate repair path failed: %v", err)
	}

	restored, err := os.ReadFile(licensePath) //nolint:gosec // test-controlled fixture path
	if err != nil {
		t.Fatalf("LICENSE was not restored: %v", err)
	}
	assertRenderedLicense(t, string(restored), "MIT License", "Test Holder")

	m, loadedRoot, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	findings, err := drift.Detect(m, loadedRoot)
	if err != nil {
		t.Fatalf("drift.Detect: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("drift after repair = %+v, want clean", findings)
	}
}

// TestRunUpdateFailsClosedOnUnknownLicense proves update/repair fail closed
// when a missing LICENSE cannot be rendered from the embedded snapshot: the
// file stays absent and rollback leaves no transaction residue.
func TestRunUpdateFailsClosedOnUnknownLicense(t *testing.T) {
	root := t.TempDir()
	const content = "stale license text\n"
	writeLicenseFixture(t, root, "NOT-A-REAL-LICENSE", content)

	licensePath := filepath.Join(root, "LICENSE")
	if err := os.WriteFile(licensePath, []byte(content), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(licensePath); err != nil {
		t.Fatalf("delete LICENSE so repair must render it: %v", err)
	}

	reporter := ui.NewPlainReporter(5, false)
	err := RunUpdate(licenseRunContext(root), reporter)
	if err == nil {
		t.Fatal("RunUpdate succeeded with an unknown license ID; want fail-closed error")
	}
	if !strings.Contains(err.Error(), "unknown SPDX license ID") {
		t.Errorf("RunUpdate error = %v, want it to name the unknown SPDX license ID", err)
	}

	if _, statErr := os.Stat(licensePath); !os.IsNotExist(statErr) {
		t.Errorf("failed update wrote LICENSE despite the unknown license ID (stat err: %v)", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, fileutil.InternalDir)); !os.IsNotExist(statErr) {
		t.Errorf("failed update left transaction residue at %s (stat err: %v)",
			filepath.Join(root, fileutil.InternalDir), statErr)
	}
}

// TestRunUpdateSkipsCleanLicense proves a clean managed LICENSE is already
// reconciled: update leaves the bytes unchanged and does not re-stamp the
// copyright year.
func TestRunUpdateSkipsCleanLicense(t *testing.T) {
	root := t.TempDir()
	const content = "Copyright (c) 2024 Test Holder\n"
	writeLicenseFixture(t, root, "MIT", content)

	licensePath := filepath.Join(root, "LICENSE")
	if err := os.WriteFile(licensePath, []byte(content), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	reporter := ui.NewPlainReporter(5, false)
	if err := RunUpdate(licenseRunContext(root), reporter); err != nil {
		t.Fatalf("clean update failed: %v", err)
	}

	after, err := os.ReadFile(licensePath) //nolint:gosec // test-controlled fixture path
	if err != nil {
		t.Fatalf("read LICENSE after clean update: %v", err)
	}
	if string(after) != content {
		t.Errorf("clean update changed LICENSE = %q, want %q", after, content)
	}
	if _, statErr := os.Stat(filepath.Join(root, fileutil.InternalDir)); !os.IsNotExist(statErr) {
		t.Errorf("clean update left transaction residue at %s (stat err: %v)",
			filepath.Join(root, fileutil.InternalDir), statErr)
	}
}

// TestRunUpdateRepairsMissingBase proves a clean LICENSE whose pristine base is
// missing is backfilled from the current bytes, so drift --require-base can be
// repaired without re-rendering the license.
func TestRunUpdateRepairsMissingBase(t *testing.T) {
	root := t.TempDir()
	const content = "stale MIT license text\n"
	writeLicenseFixture(t, root, "MIT", content)

	licensePath := filepath.Join(root, "LICENSE")
	if err := os.WriteFile(licensePath, []byte(content), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "LICENSE")

	reporter := ui.NewPlainReporter(5, false)
	if err := RunUpdate(licenseRunContext(root), reporter); err != nil {
		t.Fatalf("clean update with a missing base failed: %v", err)
	}

	base, err := os.ReadFile(basePath) //nolint:gosec // test-controlled fixture path
	if err != nil {
		t.Fatalf("missing base was not backfilled: %v", err)
	}
	if string(base) != content {
		t.Errorf("base = %q, want %q", base, content)
	}
}

// TestRunUpdateRepairsInvalidBase proves a clean LICENSE whose base bytes no
// longer match the ledger is repaired from the current bytes.
func TestRunUpdateRepairsInvalidBase(t *testing.T) {
	root := t.TempDir()
	const content = "stale MIT license text\n"
	writeLicenseFixture(t, root, "MIT", content)

	licensePath := filepath.Join(root, "LICENSE")
	if err := os.WriteFile(licensePath, []byte(content), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "LICENSE")
	if err := os.MkdirAll(filepath.Dir(basePath), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(basePath, []byte("corrupt base\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	reporter := ui.NewPlainReporter(5, false)
	if err := RunUpdate(licenseRunContext(root), reporter); err != nil {
		t.Fatalf("clean update with an invalid base failed: %v", err)
	}

	base, err := os.ReadFile(basePath) //nolint:gosec // test-controlled fixture path
	if err != nil {
		t.Fatalf("invalid base was not repaired: %v", err)
	}
	if string(base) != content {
		t.Errorf("base = %q, want %q", base, content)
	}
}

// TestRunUpdateLeavesCleanLicenseWithChangedMetadata documents the deliberate
// boundary: update repairs drift, but a clean LICENSE is not re-rendered when
// only license metadata changed. apply --with compliance is the documented path
// for changing terms.
func TestRunUpdateLeavesCleanLicenseWithChangedMetadata(t *testing.T) {
	root := t.TempDir()
	const content = "stale MIT license text\n"
	m := &types.Manifest{
		SbakeVersion: manifest.DefaultSbakeVersion,
		Templates:    []types.Template{{Name: "compliance", Path: "."}},
		Metadata: map[string]string{
			"license":          "Apache-2.0",
			"copyright_holder": "Test Holder",
		},
		ManagedFiles: map[string]string{"LICENSE": tasks.HashContent([]byte(content))},
	}
	if err := manifest.Save(m, root); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	licensePath := filepath.Join(root, "LICENSE")
	if err := os.WriteFile(licensePath, []byte(content), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	reporter := ui.NewPlainReporter(5, false)
	if err := RunUpdate(licenseRunContext(root), reporter); err != nil {
		t.Fatalf("clean update with changed metadata failed: %v", err)
	}
	got, err := os.ReadFile(licensePath) //nolint:gosec // test-controlled fixture path
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("clean LICENSE was re-rendered by update: %q, want %q", got, content)
	}
}

// TestRunUpdateRestoresMissingLicenseAfterMetadataChange proves a recorded
// compliance template is replayed when reconciliation is needed: a missing
// LICENSE is restored with the current metadata's license text.
func TestRunUpdateRestoresMissingLicenseAfterMetadataChange(t *testing.T) {
	root := t.TempDir()
	const oldContent = "stale MIT license text\n"

	// The ledger still records the MIT render while the metadata now asks for
	// Apache-2.0, exactly as after a user edits license in scbake.toml.
	m := &types.Manifest{
		SbakeVersion: manifest.DefaultSbakeVersion,
		Templates:    []types.Template{{Name: "compliance", Path: "."}},
		Metadata: map[string]string{
			"license":          "Apache-2.0",
			"copyright_holder": "Test Holder",
		},
		ManagedFiles: map[string]string{
			"LICENSE": tasks.HashContent([]byte(oldContent)),
		},
	}
	if err := manifest.Save(m, root); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	licensePath := filepath.Join(root, "LICENSE")
	if err := os.WriteFile(licensePath, []byte(oldContent), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(licensePath); err != nil {
		t.Fatalf("delete stale LICENSE so the changed metadata is rendered: %v", err)
	}

	reporter := ui.NewPlainReporter(5, false)
	if err := RunUpdate(licenseRunContext(root), reporter); err != nil {
		t.Fatalf("RunUpdate failed: %v", err)
	}

	got, err := os.ReadFile(licensePath) //nolint:gosec // test-controlled fixture path
	if err != nil {
		t.Fatalf("read rewritten LICENSE: %v", err)
	}
	text := string(got)
	assertRenderedLicense(t, text, "Apache License", "Test Holder")
	if strings.Contains(text, "stale MIT license text") {
		t.Errorf("rewritten LICENSE still contains the stale fixture text:\n%s", text)
	}
}
