// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/util/fileutil"
)

// acceptManifest is a minimal view of scbake.toml for the accept/unmanage
// regressions. It is declared locally so the black-box test file also compiles
// against the pre-fix tree (where the production manifest type has no accepted
// or unmanaged fields); the tests then fail on the missing commands instead of
// on a build error.
type acceptManifest struct {
	ManagedFiles   map[string]string `toml:"managed_files"`
	AcceptedFiles  map[string]string `toml:"accepted_files"`
	UnmanagedFiles []string          `toml:"unmanaged_files"`
}

func readAcceptManifest(t *testing.T, project string) acceptManifest {
	t.Helper()
	var m acceptManifest
	if _, err := toml.Decode(mustRead(t, filepath.Join(project, "scbake.toml")), &m); err != nil {
		t.Fatalf("decode scbake.toml: %v", err)
	}
	return m
}

// setupAcceptProject creates a fresh Go project and chdirs into it.
func setupAcceptProject(t *testing.T) string {
	t.Helper()
	work := t.TempDir()
	t.Chdir(work)
	if out, err := runCLI("new", "proj", "--lang", "go"); err != nil {
		t.Fatalf("new --lang go failed: %v\n%s", err, out)
	}
	project := filepath.Join(work, "proj")
	t.Chdir(project)
	return project
}

func requireAcceptSuccess(t *testing.T, out string, err error, command string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", command, err, out)
	}
	if strings.Contains(out, "unknown command") {
		t.Fatalf("%s is not registered: %s", command, out)
	}
}

func requireAcceptFailure(t *testing.T, out string, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("command unexpectedly succeeded; want error containing %q\n%s", want, out)
	}
	if !strings.Contains(out, want) {
		t.Fatalf("error output does not contain %q:\n%s", want, out)
	}
}

func requireDriftClean(t *testing.T) {
	t.Helper()
	out, err := runCLI("drift")
	if err != nil {
		t.Fatalf("drift reported drift: %v\n%s", err, out)
	}
}

func requireDriftDirty(t *testing.T, want ...string) string {
	t.Helper()
	out, err := runCLI("drift")
	if err == nil {
		t.Fatalf("drift unexpectedly reported clean:\n%s", out)
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("drift output missing %q:\n%s", w, out)
		}
	}
	return out
}

// freezeAcceptTreeFreeze pins every file's mtime under root so a later call can
// prove a command wrote nothing.
func freezeAcceptTree(t *testing.T, root string) map[string]time.Time {
	t.Helper()
	seen := map[string]time.Time{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		stamp := time.Unix(1_000_000_000, 0)
		//nolint:gosec // G122: path comes from Walk over the test's own t.TempDir tree
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			return err
		}
		if !info.IsDir() {
			seen[path] = stamp
		}
		return nil
	})
	if err != nil {
		t.Fatalf("freeze tree %s: %v", root, err)
	}
	return seen
}

func assertAcceptTreeFrozen(t *testing.T, root string, expected map[string]time.Time) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		want, ok := expected[path]
		if !ok {
			t.Errorf("unexpected file created: %s", path)
			return nil
		}
		if !info.ModTime().Equal(want) {
			t.Errorf("%s was rewritten (mtime %s, want %s)", path, info.ModTime(), want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk tree %s: %v", root, err)
	}
	for path := range expected {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("file disappeared: %s: %v", path, err)
		}
	}
}

// TestAcceptRebaselinesEditedMainGo is the primary #162 regression: accepting an
// edited starter keeps the bytes, the pristine ledger and base, makes drift
// clean, survives an unchanged-template update, and a later edit drifts again.
func TestAcceptRebaselinesEditedMainGo(t *testing.T) {
	project := setupAcceptProject(t)
	mainGo := filepath.Join(project, "main.go")
	basePath := filepath.Join(project, fileutil.CommittedBaseDir, "main.go")

	before := readAcceptManifest(t, project)
	pristineBase := mustRead(t, basePath)

	edited := "package main\n\nfunc main() {\n\tprintln(\"local\")\n}\n"
	mustWrite(t, mainGo, edited)

	out, err := runCLI("accept", "main.go")
	requireAcceptSuccess(t, out, err, "accept")
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("accept rewrote main.go:\n%s", got)
	}
	if got := mustRead(t, basePath); got != pristineBase {
		t.Fatalf("accept rewrote the committed base")
	}
	after := readAcceptManifest(t, project)
	if !reflect.DeepEqual(before.ManagedFiles, after.ManagedFiles) {
		t.Fatalf("accept changed managed_files: %v -> %v", before.ManagedFiles, after.ManagedFiles)
	}
	if after.AcceptedFiles["main.go"] == "" {
		t.Fatalf("accept did not record an accepted baseline: %+v", after.AcceptedFiles)
	}
	requireDriftClean(t)

	// An unchanged-template update must not rewrite or re-record the accepted edit.
	freezeMtime(t, mainGo)
	freezeMtime(t, basePath)
	out, err = runCLI("update")
	requireAcceptSuccess(t, out, err, "update")
	assertNotRewritten(t, mainGo)
	assertNotRewritten(t, basePath)
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("update rewrote the accepted edit:\n%s", got)
	}
	during := readAcceptManifest(t, project)
	if !reflect.DeepEqual(after.ManagedFiles, during.ManagedFiles) {
		t.Fatalf("update re-recorded managed_files: %v -> %v", after.ManagedFiles, during.ManagedFiles)
	}
	if during.AcceptedFiles["main.go"] != after.AcceptedFiles["main.go"] {
		t.Fatalf("update dropped the accepted baseline")
	}
	requireDriftClean(t)

	// apply on the same unchanged template is also a no-op for an accepted file.
	freezeMtime(t, mainGo)
	out, err = runCLI("apply", "--lang", "go")
	requireAcceptSuccess(t, out, err, "apply")
	assertNotRewritten(t, mainGo)
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("apply rewrote the accepted edit:\n%s", got)
	}
	if got := readAcceptManifest(t, project).AcceptedFiles["main.go"]; got == "" {
		t.Fatalf("apply dropped the accepted baseline")
	}
	requireDriftClean(t)

	// A second edit is not silently accepted: drift returns.
	mustWrite(t, mainGo, "package main\n\nfunc main() { println(\"again\") }\n")
	requireDriftDirty(t, "modified", "main.go", "accepted")
}

// TestAcceptBaseInvalidFails: an invalid committed base is a blocking drift
// finding; accept must refuse instead of claiming a clean project.
func TestAcceptBaseInvalidFails(t *testing.T) {
	project := setupAcceptProject(t)
	mainGo := filepath.Join(project, "main.go")
	basePath := filepath.Join(project, fileutil.CommittedBaseDir, "main.go")
	mustWrite(t, mainGo, "package main\n\nfunc main() {}\n")
	mustWrite(t, basePath, "corrupt base\n")

	manifestBefore := mustRead(t, filepath.Join(project, "scbake.toml"))
	out, err := runCLI("accept", "main.go")
	requireAcceptFailure(t, out, err, "invalid")
	if got := mustRead(t, filepath.Join(project, "scbake.toml")); got != manifestBefore {
		t.Fatalf("failed accept modified scbake.toml")
	}
	if m := readAcceptManifest(t, project); len(m.AcceptedFiles) != 0 {
		t.Fatalf("failed accept recorded a baseline: %+v", m.AcceptedFiles)
	}
	requireDriftDirty(t, "base-invalid", "main.go")
}

// TestAcceptMissingBaseWarns: a missing base is advisory, so accept succeeds
// with a warning while drift --require-base still fails.
func TestAcceptMissingBaseWarns(t *testing.T) {
	project := setupAcceptProject(t)
	mainGo := filepath.Join(project, "main.go")
	basePath := filepath.Join(project, fileutil.CommittedBaseDir, "main.go")
	if err := os.Remove(basePath); err != nil {
		t.Fatalf("remove base: %v", err)
	}
	mustWrite(t, mainGo, "package main\n\nfunc main() { println(1) }\n")

	out, err := runCLI("accept", "main.go")
	requireAcceptSuccess(t, out, err, "accept")
	if !strings.Contains(out, "No committed base for main.go") {
		t.Errorf("missing base did not warn:\n%s", out)
	}
	requireDriftClean(t)

	if _, err := runCLI("drift", "--require-base"); err == nil {
		t.Fatalf("drift --require-base passed with a missing base")
	}
}

// TestAcceptRejectsUnsafeInputs: accept only adopts an existing, contained,
// non-symlinked managed file. Every rejected argument leaves the manifest
// untouched.
func TestAcceptRejectsUnsafeInputs(t *testing.T) {
	project := setupAcceptProject(t)
	parent := filepath.Dir(project)
	outside := filepath.Join(parent, "outside.go")
	mustWrite(t, outside, "package outside\n")
	manifestBefore := mustRead(t, filepath.Join(project, "scbake.toml"))

	out, err := runCLI("accept", "go.mod")
	requireAcceptFailure(t, out, err, "not recorded in [managed_files]")

	out, err = runCLI("accept", "../outside.go")
	requireAcceptFailure(t, out, err, "outside the project root")

	out, err = runCLI("accept", outside)
	requireAcceptFailure(t, out, err, "outside the project root")

	out, err = runCLI("accept", ".")
	requireAcceptFailure(t, out, err, "outside the project root")

	mainGo := filepath.Join(project, "main.go")
	backup := filepath.Join(project, "main.go.bak")
	if err := os.Rename(mainGo, backup); err != nil {
		t.Fatalf("rename main.go: %v", err)
	}
	if err := os.Symlink(outside, mainGo); err != nil {
		t.Fatalf("symlink main.go: %v", err)
	}
	out, err = runCLI("accept", "main.go")
	requireAcceptFailure(t, out, err, "outside the project root")
	if err := os.Remove(mainGo); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}

	out, err = runCLI("accept", "main.go")
	requireAcceptFailure(t, out, err, "file does not exist")

	if got := mustRead(t, filepath.Join(project, "scbake.toml")); got != manifestBefore {
		t.Fatalf("a rejected accept mutated scbake.toml")
	}
}

// TestAcceptMultiplePathsAtomic: all arguments are validated before any write;
// one invalid path leaves earlier accepted entries unchanged.
func TestAcceptMultiplePathsAtomic(t *testing.T) {
	project := setupAcceptProject(t)
	mainGo := filepath.Join(project, "main.go")
	gitignore := filepath.Join(project, ".gitignore")
	mustWrite(t, mainGo, "package main\n\nfunc main() { println(2) }\n")
	mustWrite(t, gitignore, mustRead(t, gitignore)+"\n# local\n")

	out, err := runCLI("accept", "main.go", ".gitignore")
	requireAcceptSuccess(t, out, err, "accept")
	m := readAcceptManifest(t, project)
	if m.AcceptedFiles["main.go"] == "" || m.AcceptedFiles[".gitignore"] == "" {
		t.Fatalf("multiple accept did not record both keys: %+v", m.AcceptedFiles)
	}

	// Change .gitignore again, then mix a valid update with an invalid path.
	mustWrite(t, gitignore, mustRead(t, gitignore)+"\n# second\n")
	manifestBefore := mustRead(t, filepath.Join(project, "scbake.toml"))
	acceptedBefore := m.AcceptedFiles[".gitignore"]

	out, err = runCLI("accept", ".gitignore", "go.mod")
	requireAcceptFailure(t, out, err, "not recorded in [managed_files]")
	if got := mustRead(t, filepath.Join(project, "scbake.toml")); got != manifestBefore {
		t.Fatalf("atomic accept mutated scbake.toml")
	}
	if got := readAcceptManifest(t, project).AcceptedFiles[".gitignore"]; got != acceptedBefore {
		t.Fatalf("atomic accept advanced the valid key: %q -> %q", acceptedBefore, got)
	}
}

// TestAcceptDryRunWritesNothing: --dry-run validates, prints, and leaves the
// whole tree untouched (no manifest churn, no base, no lock state).
func TestAcceptDryRunWritesNothing(t *testing.T) {
	project := setupAcceptProject(t)
	mustWrite(t, filepath.Join(project, "main.go"), "package main\n\nfunc main() { println(3) }\n")
	frozen := freezeAcceptTree(t, project)

	out, err := runCLI("accept", "--dry-run", "main.go")
	requireAcceptSuccess(t, out, err, "accept --dry-run")
	if !strings.Contains(out, "Would accept main.go") {
		t.Errorf("dry-run did not describe the planned action:\n%s", out)
	}
	assertAcceptTreeFrozen(t, project, frozen)
	if _, err := os.Stat(filepath.Join(project, ".scbake")); !os.IsNotExist(err) {
		t.Errorf("dry-run created .scbake state: %v", err)
	}
}

// TestUnmanageDetachesFromDriftAndUpdate: after unmanage the path is not
// drift-gated, update never rewrites/recreates/re-records it, and the base
// shadow is gone.
func TestUnmanageDetachesFromDriftAndUpdate(t *testing.T) { //nolint:cyclop // assertion-heavy end-to-end regression test
	project := setupAcceptProject(t)
	mainGo := filepath.Join(project, "main.go")
	basePath := filepath.Join(project, fileutil.CommittedBaseDir, "main.go")
	edited := "package main\n\nfunc main() { println(\"owned\") }\n"
	mustWrite(t, mainGo, edited)

	out, err := runCLI("unmanage", "main.go")
	requireAcceptSuccess(t, out, err, "unmanage")
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("unmanage rewrote the working file:\n%s", got)
	}
	m := readAcceptManifest(t, project)
	if _, ok := m.ManagedFiles["main.go"]; ok {
		t.Fatalf("unmanaged key stayed in managed_files")
	}
	if _, ok := m.AcceptedFiles["main.go"]; ok {
		t.Fatalf("unmanaged key stayed in accepted_files")
	}
	if len(m.UnmanagedFiles) != 1 || m.UnmanagedFiles[0] != "main.go" {
		t.Fatalf("unmanaged_files = %v, want [main.go]", m.UnmanagedFiles)
	}
	if _, err := os.Stat(basePath); !os.IsNotExist(err) {
		t.Fatalf("unmanage did not remove the committed base: %v", err)
	}
	requireDriftClean(t)

	freezeMtime(t, mainGo)
	out, err = runCLI("update")
	requireAcceptSuccess(t, out, err, "update")
	assertNotRewritten(t, mainGo)
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("update rewrote the unmanaged file:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(project, "main.go.scbake-new")); !os.IsNotExist(err) {
		t.Fatalf("update wrote an artifact for an unmanaged path")
	}
	if _, err := os.Stat(basePath); !os.IsNotExist(err) {
		t.Fatalf("update recreated the committed base for an unmanaged path")
	}
	after := readAcceptManifest(t, project)
	if !reflect.DeepEqual(after.ManagedFiles, m.ManagedFiles) {
		t.Fatalf("update re-recorded an unmanaged path: %v -> %v", m.ManagedFiles, after.ManagedFiles)
	}
	if !reflect.DeepEqual(after.UnmanagedFiles, m.UnmanagedFiles) {
		t.Fatalf("update dropped the unmanaged list: %v", after.UnmanagedFiles)
	}

	// A missing unmanaged file must stay missing.
	if err := os.Remove(mainGo); err != nil {
		t.Fatalf("remove main.go: %v", err)
	}
	out, err = runCLI("update")
	requireAcceptSuccess(t, out, err, "update")
	if _, err := os.Stat(mainGo); !os.IsNotExist(err) {
		t.Fatalf("update recreated a missing unmanaged file: %v", err)
	}
	requireDriftClean(t)
}

// TestUnmanageIdempotentAndAtomic: a failed batch changes nothing, and a second
// unmanage of the same key succeeds without another manifest write.
func TestUnmanageIdempotentAndAtomic(t *testing.T) {
	project := setupAcceptProject(t)
	basePath := filepath.Join(project, fileutil.CommittedBaseDir, "main.go")
	manifestPath := filepath.Join(project, "scbake.toml")
	manifestBefore := mustRead(t, manifestPath)

	out, err := runCLI("unmanage", "main.go", "go.mod")
	requireAcceptFailure(t, out, err, "not recorded in [managed_files]")
	if got := mustRead(t, manifestPath); got != manifestBefore {
		t.Fatalf("atomic unmanage mutated scbake.toml")
	}
	if _, err := os.Stat(basePath); err != nil {
		t.Fatalf("atomic unmanage removed the base of the valid key: %v", err)
	}

	out, err = runCLI("unmanage", "main.go")
	requireAcceptSuccess(t, out, err, "unmanage")
	first := mustRead(t, manifestPath)

	out, err = runCLI("unmanage", "main.go")
	requireAcceptSuccess(t, out, err, "unmanage again")
	if !strings.Contains(out, "already unmanaged") {
		t.Errorf("second unmanage did not report idempotency:\n%s", out)
	}
	if got := mustRead(t, manifestPath); got != first {
		t.Fatalf("idempotent unmanage rewrote scbake.toml")
	}
}

// TestUnmanageDryRunWritesNothing: --dry-run reports the planned base removal
// without touching the manifest, base, or working file.
func TestUnmanageDryRunWritesNothing(t *testing.T) {
	project := setupAcceptProject(t)
	basePath := filepath.Join(project, fileutil.CommittedBaseDir, "main.go")
	frozen := freezeAcceptTree(t, project)

	out, err := runCLI("unmanage", "--dry-run", "main.go")
	requireAcceptSuccess(t, out, err, "unmanage --dry-run")
	if !strings.Contains(out, "Would unmanage main.go") {
		t.Errorf("dry-run did not describe the planned detach:\n%s", out)
	}
	assertAcceptTreeFrozen(t, project, frozen)
	if _, err := os.Stat(basePath); err != nil {
		t.Fatalf("dry-run removed the committed base: %v", err)
	}
	if m := readAcceptManifest(t, project); m.ManagedFiles["main.go"] == "" {
		t.Fatalf("dry-run unmanaged main.go")
	}
	if _, err := os.Stat(filepath.Join(project, ".scbake")); !os.IsNotExist(err) {
		t.Errorf("dry-run created .scbake state: %v", err)
	}
}

// TestUnmanageRootGitignoreRefusedNestedAllowed: the root .gitignore carries
// scbake's internal-state block and is refused; a nested .gitignore is a normal
// managed file and can be detached.
func TestUnmanageRootGitignoreRefusedNestedAllowed(t *testing.T) { //nolint:cyclop // assertion-heavy regression test
	project := setupAcceptProject(t)
	manifestPath := filepath.Join(project, "scbake.toml")
	rootBase := filepath.Join(project, fileutil.CommittedBaseDir, ".gitignore.scbake-shadow")
	manifestBefore := mustRead(t, manifestPath)

	out, err := runCLI("unmanage", ".gitignore")
	requireAcceptFailure(t, out, err, "root .gitignore cannot be unmanaged")
	if got := mustRead(t, manifestPath); got != manifestBefore {
		t.Fatalf("refused root .gitignore unmanage mutated scbake.toml")
	}
	if _, err := os.Stat(rootBase); err != nil {
		t.Fatalf("refused unmanage removed the root .gitignore base: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o750); err != nil {
		t.Fatalf("create .git marker: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project, "services", "api"), 0o750); err != nil {
		t.Fatalf("mkdir services/api: %v", err)
	}
	if out, err := runCLI("apply", "--lang", "go", "services/api"); err != nil {
		t.Fatalf("apply subproject failed: %v\n%s", err, out)
	}
	nestedKey := "services/api/.gitignore"
	nestedBase := filepath.Join(project, fileutil.CommittedBaseDir, "services", "api", ".gitignore.scbake-shadow")
	nestedFile := filepath.Join(project, "services", "api", ".gitignore")
	nestedBytes := mustRead(t, nestedFile)
	if m := readAcceptManifest(t, project); m.ManagedFiles[nestedKey] == "" {
		t.Fatalf("nested .gitignore not recorded as %q", nestedKey)
	}
	if _, err := os.Stat(nestedBase); err != nil {
		t.Fatalf("nested base missing before unmanage: %v", err)
	}

	out, err = runCLI("unmanage", nestedKey)
	requireAcceptSuccess(t, out, err, "unmanage nested .gitignore")
	if got := mustRead(t, nestedFile); got != nestedBytes {
		t.Fatalf("nested unmanage rewrote the working file")
	}
	if _, err := os.Stat(nestedBase); !os.IsNotExist(err) {
		t.Fatalf("nested unmanage did not remove the base shadow: %v", err)
	}
	if m := readAcceptManifest(t, project); m.ManagedFiles[nestedKey] != "" {
		t.Fatalf("nested key stayed managed")
	}
}

// TestApplySkipsUnmanagedUntilForce: apply must not silently re-manage a
// detached path; only --force does, and dry-run agrees.
func TestApplySkipsUnmanagedUntilForce(t *testing.T) { //nolint:cyclop // assertion-heavy regression test
	project := setupAcceptProject(t)
	mainGo := filepath.Join(project, "main.go")
	edited := "package main\n\nfunc main() { println(\"detached\") }\n"
	mustWrite(t, mainGo, edited)
	if out, err := runCLI("unmanage", "main.go"); err != nil {
		t.Fatalf("unmanage: %v\n%s", err, out)
	}

	out, err := runCLI("apply", "--lang", "go")
	requireAcceptSuccess(t, out, err, "apply")
	if !strings.Contains(out, "Skipping unmanaged main.go") {
		t.Errorf("apply did not report the skip:\n%s", out)
	}
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("apply rewrote an unmanaged file:\n%s", got)
	}
	if m := readAcceptManifest(t, project); m.ManagedFiles["main.go"] != "" {
		t.Fatalf("apply silently re-managed main.go")
	}

	// A conflict strategy is not a re-manage opt-in either.
	out, err = runCLI("apply", "--lang", "go", "--conflict-strategy", "overwrite")
	requireAcceptSuccess(t, out, err, "apply overwrite")
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("apply --conflict-strategy overwrite rewrote an unmanaged file:\n%s", got)
	}
	if m := readAcceptManifest(t, project); m.ManagedFiles["main.go"] != "" {
		t.Fatalf("apply --conflict-strategy overwrite silently re-managed main.go")
	}

	frozen := freezeAcceptTree(t, project)
	out, err = runCLI("apply", "--lang", "go", "--dry-run")
	requireAcceptSuccess(t, out, err, "apply --dry-run")
	if !strings.Contains(out, "Skipping unmanaged main.go") {
		t.Errorf("apply dry-run did not report the skip:\n%s", out)
	}
	assertAcceptTreeFrozen(t, project, frozen)

	out, err = runCLI("apply", "--lang", "go", "--force")
	requireAcceptSuccess(t, out, err, "apply --force")
	m := readAcceptManifest(t, project)
	if len(m.UnmanagedFiles) != 0 {
		t.Fatalf("apply --force did not clear unmanaged_files: %v", m.UnmanagedFiles)
	}
	if m.ManagedFiles["main.go"] == "" {
		t.Fatalf("apply --force did not re-record main.go")
	}
	if got := mustRead(t, mainGo); strings.Contains(got, "detached") {
		t.Fatalf("apply --force did not write canonical content:\n%s", got)
	}
	requireDriftClean(t)
}

// TestUpdateAndRepairNeverReManage: update and drift --repair are not a
// re-manage path even under an overwrite strategy.
func TestUpdateAndRepairNeverReManage(t *testing.T) {
	project := setupAcceptProject(t)
	mainGo := filepath.Join(project, "main.go")
	edited := "package main\n\nfunc main() { println(\"keep\") }\n"
	mustWrite(t, mainGo, edited)
	if out, err := runCLI("unmanage", "main.go"); err != nil {
		t.Fatalf("unmanage: %v\n%s", err, out)
	}
	before := readAcceptManifest(t, project)

	freezeMtime(t, mainGo)
	out, err := runCLI("drift", "--repair", "--conflict-strategy", "overwrite")
	requireAcceptSuccess(t, out, err, "drift --repair")
	assertNotRewritten(t, mainGo)
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("drift --repair rewrote the unmanaged file:\n%s", got)
	}
	out, err = runCLI("update", "--conflict-strategy", "overwrite")
	requireAcceptSuccess(t, out, err, "update --conflict-strategy overwrite")
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("update overwrite rewrote the unmanaged file:\n%s", got)
	}
	out, err = runCLI("update", "--force")
	requireAcceptSuccess(t, out, err, "update --force")
	if got := mustRead(t, mainGo); got != edited {
		t.Fatalf("update --force rewrote the unmanaged file:\n%s", got)
	}
	after := readAcceptManifest(t, project)
	if !reflect.DeepEqual(before.ManagedFiles, after.ManagedFiles) {
		t.Fatalf("repair/update re-recorded an unmanaged path: %v -> %v", before.ManagedFiles, after.ManagedFiles)
	}
	if !reflect.DeepEqual(before.UnmanagedFiles, after.UnmanagedFiles) {
		t.Fatalf("repair/update dropped the unmanaged list: %v", after.UnmanagedFiles)
	}
	if _, err := os.Stat(filepath.Join(project, fileutil.CommittedBaseDir, "main.go")); !os.IsNotExist(err) {
		t.Fatalf("repair/update recreated the committed base")
	}
}

// TestUpdateDryRunOmitsUnmanagedTask: the plan filter, not an execution skip,
// keeps the dry-run preview honest.
func TestUpdateDryRunOmitsUnmanagedTask(t *testing.T) {
	project := setupAcceptProject(t)

	out, err := runCLI("update", "--dry-run")
	requireAcceptSuccess(t, out, err, "update --dry-run")
	if !strings.Contains(out, "Create main.go") {
		t.Fatalf("control: managed main.go is missing from the dry-run plan:\n%s", out)
	}

	if out, err := runCLI("unmanage", "main.go"); err != nil {
		t.Fatalf("unmanage: %v\n%s", err, out)
	}
	frozen := freezeAcceptTree(t, project)
	out, err = runCLI("update", "--dry-run")
	requireAcceptSuccess(t, out, err, "update --dry-run after unmanage")
	if !strings.Contains(out, "Skipping unmanaged main.go") {
		t.Errorf("dry-run did not report the skipped unmanaged task:\n%s", out)
	}
	if strings.Contains(out, "Create main.go") {
		t.Errorf("dry-run still lists the unmanaged task:\n%s", out)
	}
	assertAcceptTreeFrozen(t, project, frozen)
}

// TestAcceptOverwriteClearsAcceptance: a canonical overwrite restores the
// template, drops the accepted baseline, and leaves drift clean.
func TestAcceptOverwriteClearsAcceptance(t *testing.T) {
	project := setupAcceptProject(t)
	mainGo := filepath.Join(project, "main.go")
	pristine := mustRead(t, mainGo)
	mustWrite(t, mainGo, "package main\n\nfunc main() { println(\"local\") }\n")

	out, err := runCLI("accept", "main.go")
	requireAcceptSuccess(t, out, err, "accept")
	if readAcceptManifest(t, project).AcceptedFiles["main.go"] == "" {
		t.Fatalf("accept did not record a baseline")
	}

	out, err = runCLI("update", "--conflict-strategy", "overwrite")
	requireAcceptSuccess(t, out, err, "update overwrite")
	if got := mustRead(t, mainGo); got != pristine {
		t.Fatalf("overwrite did not restore the template:\n%s", got)
	}
	if got := readAcceptManifest(t, project).AcceptedFiles["main.go"]; got != "" {
		t.Fatalf("overwrite left a stale accepted baseline: %q", got)
	}
	requireDriftClean(t)
}

// TestAcceptTemplateChangeClearsAcceptance: a clean template merge writes
// canonical/merged content, clears acceptance, and drift returns until the new
// content is accepted.
func TestAcceptTemplateChangeClearsAcceptance(t *testing.T) {
	_, overrideDir := setupUpdateProject(t, "alpha\nbeta\ngamma\n")
	makefile := "Makefile"
	mustWrite(t, makefile, "ALPHA\nbeta\ngamma\n")

	out, err := runCLI("accept", makefile)
	requireAcceptSuccess(t, out, err, "accept")
	if readAcceptManifest(t, ".").AcceptedFiles[makefile] == "" {
		t.Fatalf("accept did not record the Makefile baseline")
	}
	requireDriftClean(t)

	// Change a disjoint line in the template so the three-way merge is clean.
	mustWrite(t, filepath.Join(overrideDir, "makefile.tpl"), "alpha\nbeta\nGAMMA\n")
	out, err = runCLI("update", "--template-dir", overrideDir)
	requireAcceptSuccess(t, out, err, "update after template change")
	if got := mustRead(t, makefile); !strings.Contains(got, "ALPHA") || !strings.Contains(got, "GAMMA") {
		t.Fatalf("clean merge did not combine local and template changes:\n%s", got)
	}
	if got := readAcceptManifest(t, ".").AcceptedFiles[makefile]; got != "" {
		t.Fatalf("canonical merge did not clear the accepted baseline: %q", got)
	}
	requireDriftDirty(t, "modified", "Makefile")

	out, err = runCLI("accept", makefile)
	requireAcceptSuccess(t, out, err, "re-accept after template change")
	requireDriftClean(t)
}

// TestAcceptMonorepoRootRelativeKeys: keys are root-relative regardless of the
// invocation directory, and update from the subproject preserves the edit.
func TestAcceptMonorepoRootRelativeKeys(t *testing.T) {
	project := setupAcceptProject(t)
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o750); err != nil {
		t.Fatalf("create .git marker: %v", err)
	}
	if out, err := runCLI("apply", "--lang", "go", "services/api"); err != nil {
		t.Fatalf("apply subproject: %v\n%s", err, out)
	}
	subMain := filepath.Join(project, "services", "api", "main.go")
	edited := "package main\n\nfunc main() { println(\"api\") }\n"
	mustWrite(t, subMain, edited)

	out, err := runCLI("accept", "services/api/main.go")
	requireAcceptSuccess(t, out, err, "accept root-relative")
	if readAcceptManifest(t, project).AcceptedFiles["services/api/main.go"] == "" {
		t.Fatalf("accept did not use a root-relative key")
	}
	requireDriftClean(t)

	t.Chdir(filepath.Join(project, "services", "api"))
	freezeMtime(t, subMain)
	out, err = runCLI("update")
	requireAcceptSuccess(t, out, err, "update from subproject")
	assertNotRewritten(t, subMain)
	if got := mustRead(t, subMain); got != edited {
		t.Fatalf("subproject update rewrote the accepted edit:\n%s", got)
	}
	requireDriftClean(t)

	// A second edit accepted from the subproject still resolves root-relative.
	mustWrite(t, subMain, "package main\n\nfunc main() { println(\"api2\") }\n")
	out, err = runCLI("accept", "main.go")
	requireAcceptSuccess(t, out, err, "accept from subproject")
	if readAcceptManifest(t, project).AcceptedFiles["main.go"] != "" {
		t.Fatalf("subproject accept recorded a subproject-relative key")
	}
	if got := readAcceptManifest(t, project).AcceptedFiles["services/api/main.go"]; got == "" {
		t.Fatalf("subproject accept did not update the root-relative key")
	}
	requireDriftClean(t)
}

// TestAcceptUnmanageBusyExits75: both mutators join the per-root lock; dry-run
// and drift stay lock-free.
func TestAcceptUnmanageBusyExits75(t *testing.T) {
	project := setupAcceptProject(t)
	manifestPath := filepath.Join(project, "scbake.toml")
	manifestBefore := mustRead(t, manifestPath)

	lock, err := transaction.AcquireRootLock(project)
	if err != nil {
		t.Fatalf("AcquireRootLock: %v", err)
	}
	defer func() { _ = lock.Release() }()

	for _, args := range [][]string{{"accept", "main.go"}, {"unmanage", "main.go"}} {
		out, err := runCLI(args...)
		if err == nil {
			t.Fatalf("%v succeeded while the root lock was held:\n%s", args, out)
		}
		code, ok := exitCodeOf(err)
		if !ok || code != fileutil.ExitBusy {
			t.Fatalf("%v exit code = %d (ok=%v), want %d\n%s", args, code, ok, fileutil.ExitBusy, out)
		}
	}

	// drift is read-only and must not contend for the root lock.
	if out, err := runCLI("drift"); err != nil {
		t.Fatalf("drift must stay lock-free: %v\n%s", err, out)
	}

	// A dry run validates without taking the lock either.
	mustWrite(t, filepath.Join(project, "main.go"), "package main\n\nfunc main() { println(\"pending\") }\n")
	out, err := runCLI("accept", "--dry-run", "main.go")
	requireAcceptSuccess(t, out, err, "accept --dry-run while locked")
	if !strings.Contains(out, "Would accept main.go") {
		t.Errorf("locked dry-run did not validate:\n%s", out)
	}
	if got := mustRead(t, manifestPath); got != manifestBefore {
		t.Fatalf("lock-contended/dry-run commands mutated scbake.toml")
	}
}

// TestDriftReadOnlyWithAcceptedState: drift with accepted and unmanaged state
// writes nothing and creates no lock/journal state.
func TestDriftReadOnlyWithAcceptedState(t *testing.T) {
	project := setupAcceptProject(t)
	mainGo := filepath.Join(project, "main.go")
	mustWrite(t, mainGo, "package main\n\nfunc main() { println(\"accepted\") }\n")
	if out, err := runCLI("accept", "main.go"); err != nil {
		t.Fatalf("accept: %v\n%s", err, out)
	}
	frozen := freezeAcceptTree(t, project)

	requireDriftClean(t)
	assertAcceptTreeFrozen(t, project, frozen)
	mustWrite(t, mainGo, "package main\n\nfunc main() { println(\"drifted\") }\n")
	// The edit above is intentional, not drift-command output; freeze again.
	frozen = freezeAcceptTree(t, project)
	requireDriftDirty(t, "modified", "accepted")
	assertAcceptTreeFrozen(t, project, frozen)
	if _, err := os.Stat(filepath.Join(project, ".scbake")); !os.IsNotExist(err) {
		t.Errorf("drift created .scbake state: %v", err)
	}
}
