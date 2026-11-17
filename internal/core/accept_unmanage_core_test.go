// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"scbake/pkg/templates/compliance"
)

var manageTestFrozen = time.Unix(1_000_000_000, 0)

func manageRunContext(root string) RunContext {
	return RunContext{TargetPath: root, ManifestPathArg: ".", Ctx: context.Background()}
}

// writeManageFixture creates a project whose ledger and committed base agree on
// the supplied pristine content per key.
func writeManageFixture(t *testing.T, root string, pristine map[string]string) *types.Manifest {
	t.Helper()
	managed := make(map[string]string, len(pristine))
	for key, content := range pristine {
		managed[key] = tasks.HashContent([]byte(content))
	}
	m := &types.Manifest{
		SbakeVersion: manifest.DefaultSbakeVersion,
		ManagedFiles: managed,
	}
	if err := manifest.Save(m, root); err != nil {
		t.Fatalf("save fixture manifest: %v", err)
	}
	for key, content := range pristine {
		basePath := filepath.Join(root, tasks.BaseShadowRelPath(key))
		if err := os.MkdirAll(filepath.Dir(basePath), 0o750); err != nil {
			t.Fatalf("mkdir base: %v", err)
		}
		if err := os.WriteFile(basePath, []byte(content), 0o600); err != nil {
			t.Fatalf("write base: %v", err)
		}
	}
	return m
}

func loadManageTestManifest(t *testing.T, root string) *types.Manifest {
	t.Helper()
	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	return loaded
}

// TestFilterUnmanagedTasks pins the plan-level filter that keeps dry-run and
// the real run in agreement.
func TestFilterUnmanagedTasks(t *testing.T) {
	create := &tasks.CreateTemplateTask{OutputPath: "main.go"}
	license := &compliance.LicenseTask{}
	exec := &tasks.ExecCommandTask{Cmd: "go"}
	plan := []types.Task{create, license, exec}

	m := &types.Manifest{UnmanagedFiles: []string{"main.go", "services/api/LICENSE"}}

	rootPlan := filterUnmanagedTasks(plan, m, ".")
	if len(rootPlan) != 2 {
		t.Fatalf("root filter kept %d tasks, want 2: %T", len(rootPlan), rootPlan)
	}
	if rootPlan[0] != types.Task(license) || rootPlan[1] != types.Task(exec) {
		t.Errorf("root filter kept the wrong tasks: %T, %T", rootPlan[0], rootPlan[1])
	}

	subPlan := filterUnmanagedTasks(plan, m, "services/api")
	if len(subPlan) != 2 {
		t.Fatalf("subproject filter kept %d tasks, want 2: %T", len(subPlan), subPlan)
	}
	if subPlan[0] != types.Task(create) || subPlan[1] != types.Task(exec) {
		t.Errorf("subproject filter kept the wrong tasks: %T, %T", subPlan[0], subPlan[1])
	}

	// A differently-scoped key is a different path: no cross-project filtering.
	other := filterUnmanagedTasks([]types.Task{create}, m, "services/other")
	if len(other) != 1 {
		t.Errorf("filtered a same-basename task in another project")
	}

	if got := filterUnmanagedTasks(plan, nil, "."); len(got) != len(plan) {
		t.Errorf("nil manifest filtered tasks")
	}
	if got := filterUnmanagedTasks(plan, &types.Manifest{}, "."); len(got) != len(plan) {
		t.Errorf("empty unmanaged list filtered tasks")
	}
}

// TestPersistAcceptedUnmanagedReplaceSemantics pins replace-not-merge: a
// canonical write clears acceptance and an apply --force clears unmanaged
// entries durably.
func TestPersistAcceptedUnmanagedReplaceSemantics(t *testing.T) {
	m := &types.Manifest{
		AcceptedFiles:  map[string]string{"stale.txt": "stale", "keep.txt": "keep"},
		UnmanagedFiles: []string{"stale.txt", "keep.txt"},
	}
	tc := types.TaskContext{Manifest: &types.Manifest{
		AcceptedFiles:  map[string]string{"keep.txt": "new"},
		UnmanagedFiles: []string{"z.txt", "a.txt", "a.txt"},
	}}
	persistAcceptedFiles(m, tc)
	persistUnmanagedFiles(m, tc)

	if !reflect.DeepEqual(m.AcceptedFiles, map[string]string{"keep.txt": "new"}) {
		t.Fatalf("accepted files merged instead of replaced: %v", m.AcceptedFiles)
	}
	if !reflect.DeepEqual(m.UnmanagedFiles, []string{"a.txt", "z.txt"}) {
		t.Fatalf("unmanaged files = %v, want sorted replacement", m.UnmanagedFiles)
	}

	// An empty context clears both: deletion must persist.
	persistAcceptedFiles(m, types.TaskContext{Manifest: &types.Manifest{}})
	persistUnmanagedFiles(m, types.TaskContext{Manifest: &types.Manifest{}})
	if len(m.AcceptedFiles) != 0 {
		t.Errorf("accepted files were not cleared: %v", m.AcceptedFiles)
	}
	if len(m.UnmanagedFiles) != 0 {
		t.Errorf("unmanaged files were not cleared: %v", m.UnmanagedFiles)
	}

	// A nil context manifest leaves the real manifest alone.
	m.AcceptedFiles = map[string]string{"keep.txt": "x"}
	m.UnmanagedFiles = []string{"a.txt"}
	persistAcceptedFiles(m, types.TaskContext{})
	persistUnmanagedFiles(m, types.TaskContext{})
	if m.AcceptedFiles["keep.txt"] != "x" || len(m.UnmanagedFiles) != 1 {
		t.Errorf("nil context manifest mutated state: %v %v", m.AcceptedFiles, m.UnmanagedFiles)
	}
}

// TestRunAcceptUnmanageRoundTrip exercises the core mutation without the CLI:
// accept leaves bytes/base alone, unmanage detaches and removes the base, and
// the second unmanage is a no-op.
func TestRunAcceptUnmanageRoundTrip(t *testing.T) { //nolint:cyclop // assertion-heavy round-trip regression test
	root := t.TempDir()
	pristine := []byte("pristine a.txt\n")
	m := writeManageFixture(t, root, map[string]string{"a.txt": string(pristine)})
	path := filepath.Join(root, "a.txt")
	edited := []byte("accepted edit\n")
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatalf("write edit: %v", err)
	}
	basePath := filepath.Join(root, tasks.BaseShadowRelPath("a.txt"))
	baseBefore, _ := os.ReadFile(basePath) //nolint:gosec // test-controlled fixture path under t.TempDir()

	if err := RunAccept(manageRunContext(root), []string{"a.txt"}); err != nil {
		t.Fatalf("RunAccept: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(edited) { //nolint:gosec // test-controlled fixture path under t.TempDir()
		t.Fatalf("accept rewrote the file: %q", got)
	}
	if got, _ := os.ReadFile(basePath); string(got) != string(baseBefore) { //nolint:gosec // test-controlled fixture path under t.TempDir()
		t.Fatalf("accept rewrote the base")
	}
	loaded := loadManageTestManifest(t, root)
	if loaded.ManagedFiles["a.txt"] != m.ManagedFiles["a.txt"] {
		t.Errorf("accept moved the pristine hash: %v", loaded.ManagedFiles)
	}
	if loaded.AcceptedFiles["a.txt"] != tasks.HashContent(edited) {
		t.Errorf("accept baseline = %q", loaded.AcceptedFiles)
	}

	if err := RunUnmanage(manageRunContext(root), []string{"a.txt"}); err != nil {
		t.Fatalf("RunUnmanage: %v", err)
	}
	loaded = loadManageTestManifest(t, root)
	if _, ok := loaded.ManagedFiles["a.txt"]; ok {
		t.Errorf("unmanage kept the managed row")
	}
	if _, ok := loaded.AcceptedFiles["a.txt"]; ok {
		t.Errorf("unmanage kept the accepted row")
	}
	if !reflect.DeepEqual(loaded.UnmanagedFiles, []string{"a.txt"}) {
		t.Errorf("unmanaged_files = %v", loaded.UnmanagedFiles)
	}
	if _, err := os.Stat(basePath); !os.IsNotExist(err) {
		t.Errorf("unmanage did not remove the base: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(edited) { //nolint:gosec // test-controlled fixture path under t.TempDir()
		t.Errorf("unmanage changed the working file: %q", got)
	}

	manifestAfter := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName))
	if err := RunUnmanage(manageRunContext(root), []string{"a.txt"}); err != nil {
		t.Fatalf("idempotent RunUnmanage: %v", err)
	}
	if got := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName)); got != manifestAfter {
		t.Errorf("idempotent unmanage rewrote the manifest")
	}

	// Accepting a detached path is rejected.
	if err := RunAccept(manageRunContext(root), []string{"a.txt"}); err == nil {
		t.Errorf("accept succeeded on a detached path")
	}
}

// TestRunAcceptDryRunWritesNothing: dry-run validates the arguments but leaves
// every file, including the manifest and base, untouched.
func TestRunAcceptDryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("pristine a.txt\n")
	writeManageFixture(t, root, map[string]string{"a.txt": string(pristine)})
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("edited\n"), 0o600); err != nil {
		t.Fatalf("write edit: %v", err)
	}
	manifestPath := filepath.Join(root, fileutil.ManifestFileName)
	basePath := filepath.Join(root, tasks.BaseShadowRelPath("a.txt"))
	for _, p := range []string{path, manifestPath, basePath} {
		if err := os.Chtimes(p, manageTestFrozen, manageTestFrozen); err != nil {
			t.Fatalf("chtimes %s: %v", p, err)
		}
	}
	before := mustManageRead(t, manifestPath)

	rc := manageRunContext(root)
	rc.DryRun = true
	if err := RunAccept(rc, []string{"a.txt"}); err != nil {
		t.Fatalf("dry-run RunAccept: %v", err)
	}
	if got := mustManageRead(t, manifestPath); got != before {
		t.Fatalf("dry-run accept rewrote the manifest")
	}
	for _, p := range []string{path, manifestPath, basePath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if !info.ModTime().Equal(manageTestFrozen) {
			t.Errorf("%s was rewritten by dry-run", p)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".scbake")); !os.IsNotExist(err) {
		t.Errorf("dry-run accept created .scbake state")
	}
}

// TestRunManageNoOpChecksPending: a real no-op must not report success on a
// project with an interrupted transaction, matching --dry-run and drift.
func TestRunManageNoOpChecksPending(t *testing.T) {
	t.Run("accept no-op", func(t *testing.T) {
		root := t.TempDir()
		pristine := []byte("pristine a.txt\n")
		writeManageFixture(t, root, map[string]string{"a.txt": string(pristine)})
		if err := os.WriteFile(filepath.Join(root, "a.txt"), pristine, 0o600); err != nil {
			t.Fatalf("write pristine: %v", err)
		}
		writeDeadJournal(t, root)
		before := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName))

		err := RunAccept(manageRunContext(root), []string{"a.txt"})
		if !errors.Is(err, transaction.ErrIncompleteTransaction) {
			t.Fatalf("accept no-op error = %v, want ErrIncompleteTransaction", err)
		}
		if got := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName)); got != before {
			t.Errorf("no-op accept rewrote the manifest")
		}
	})

	t.Run("unmanage no-op", func(t *testing.T) {
		root := t.TempDir()
		m := &types.Manifest{
			SbakeVersion:   manifest.DefaultSbakeVersion,
			UnmanagedFiles: []string{"a.txt"},
		}
		if err := manifest.Save(m, root); err != nil {
			t.Fatalf("save manifest: %v", err)
		}
		writeDeadJournal(t, root)
		before := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName))

		err := RunUnmanage(manageRunContext(root), []string{"a.txt"})
		if !errors.Is(err, transaction.ErrIncompleteTransaction) {
			t.Fatalf("unmanage no-op error = %v, want ErrIncompleteTransaction", err)
		}
		if got := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName)); got != before {
			t.Errorf("no-op unmanage rewrote the manifest")
		}
	})
}

// writeDeadJournal installs an uncommitted journal owned by a PID that cannot
// be running, so the project is in the needs-recovery state.
func writeDeadJournal(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, "tx-999999")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir pending transaction: %v", err)
	}
	header := []byte(`{"version":2,"pid":1073741824}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), header, 0o600); err != nil {
		t.Fatalf("write pending journal: %v", err)
	}
}

// TestRunAcceptBaseInvalidFails: a corrupt base blocks acceptance.
func TestRunAcceptBaseInvalidFails(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("pristine a.txt\n")
	writeManageFixture(t, root, map[string]string{"a.txt": string(pristine)})
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("edited\n"), 0o600); err != nil {
		t.Fatalf("write edit: %v", err)
	}
	basePath := filepath.Join(root, tasks.BaseShadowRelPath("a.txt"))
	if err := os.WriteFile(basePath, []byte("corrupt\n"), 0o600); err != nil {
		t.Fatalf("corrupt base: %v", err)
	}
	before := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName))

	err := RunAccept(manageRunContext(root), []string{"a.txt"})
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("RunAccept error = %v, want invalid base", err)
	}
	if got := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName)); got != before {
		t.Errorf("failed accept rewrote the manifest")
	}
}

// TestRunAcceptAtomicBatch: one invalid argument leaves the valid one untouched.
func TestRunAcceptAtomicBatch(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("pristine a.txt\n")
	writeManageFixture(t, root, map[string]string{"a.txt": string(pristine)})
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("edited "+name+"\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	before := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName))

	err := RunAccept(manageRunContext(root), []string{"a.txt", "b.txt"})
	if err == nil {
		t.Fatalf("batch accept with an untracked path succeeded")
	}
	if got := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName)); got != before {
		t.Errorf("failed batch rewrote the manifest")
	}
	if m := loadManageTestManifest(t, root); len(m.AcceptedFiles) != 0 {
		t.Errorf("partial batch recorded state: %v", m.AcceptedFiles)
	}
}

// TestRunUnmanageBaseDeletionSafety: a non-regular committed base fails closed
// before the manifest is touched.
func TestRunUnmanageBaseDeletionSafety(t *testing.T) { //nolint:cyclop // table-driven safety regression test
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			writeManageFixture(t, root, map[string]string{"a.txt": "pristine\n"})
			basePath := filepath.Join(root, tasks.BaseShadowRelPath("a.txt"))
			if err := os.Remove(basePath); err != nil {
				t.Fatalf("remove base: %v", err)
			}
			if kind == "directory" {
				if err := os.Mkdir(basePath, 0o750); err != nil {
					t.Fatalf("mkdir base: %v", err)
				}
			} else {
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
					t.Fatalf("write outside: %v", err)
				}
				if err := os.Symlink(outside, basePath); err != nil {
					t.Fatalf("symlink base: %v", err)
				}
			}
			before := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName))
			err := RunUnmanage(manageRunContext(root), []string{"a.txt"})
			if err == nil || (!strings.Contains(err.Error(), "not a regular file") &&
				!strings.Contains(err.Error(), "invalid base shadow path")) {
				t.Fatalf("RunUnmanage error = %v, want a base-safety refusal", err)
			}
			if got := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName)); got != before {
				t.Errorf("refused unmanage rewrote the manifest")
			}
			if _, err := os.Lstat(basePath); err != nil {
				t.Errorf("refused unmanage removed the base: %v", err)
			}
		})
	}
}

// TestRemoveUnmanagedBasesRollbackRestoresBase injects the failure path that
// runManage relies on: a base removal is tracked before it happens, so the
// deferred rollback restores the pristine bytes if the manifest save fails.
func TestRemoveUnmanagedBasesRollbackRestoresBase(t *testing.T) {
	root := t.TempDir()
	basePath := filepath.Join(root, tasks.BaseShadowRelPath("a.txt"))
	if err := os.MkdirAll(filepath.Dir(basePath), 0o750); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	pristine := "pristine rendered bytes\n"
	if err := os.WriteFile(basePath, []byte(pristine), 0o600); err != nil {
		t.Fatalf("write base: %v", err)
	}

	tx, cleanup, err := setupTransaction(root, false, false)
	if err != nil {
		t.Fatalf("setupTransaction: %v", err)
	}
	actions := []manageAction{{key: "a.txt", baseAbs: basePath, removeBase: true}}
	if err := removeUnmanagedBases(tx, actions); err != nil {
		t.Fatalf("removeUnmanagedBases: %v", err)
	}
	if _, err := os.Stat(basePath); !os.IsNotExist(err) {
		t.Fatalf("base was not removed before rollback: %v", err)
	}

	// The deferred cleanup is the rollback path taken when manifest.Save fails.
	cleanup()
	got, err := os.ReadFile(basePath) //nolint:gosec // test-controlled fixture path under t.TempDir()
	if err != nil {
		t.Fatalf("rollback did not restore the base: %v", err)
	}
	if string(got) != pristine {
		t.Fatalf("rollback restored %q, want %q", got, pristine)
	}
}

// TestRunManageLockBeforeLoad: a busy root fails with ErrBusy even when the
// manifest is invalid, proving the lock is taken before decoding.
func TestRunManageLockBeforeLoad(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte("this is not toml [[["), 0o600); err != nil {
		t.Fatalf("write invalid manifest: %v", err)
	}
	lock, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("AcquireRootLock: %v", err)
	}
	err = RunAccept(manageRunContext(root), []string{"a.txt"})
	if !errors.Is(err, transaction.ErrBusy) {
		t.Fatalf("RunAccept while locked = %v, want ErrBusy (lock-before-load)", err)
	}
	err = RunUnmanage(manageRunContext(root), []string{"a.txt"})
	if !errors.Is(err, transaction.ErrBusy) {
		t.Fatalf("RunUnmanage while locked = %v, want ErrBusy (lock-before-load)", err)
	}

	// Without the lock the invalid manifest is the failure, not busy.
	if err := lock.Release(); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	err = RunAccept(manageRunContext(root), []string{"a.txt"})
	if err == nil || errors.Is(err, transaction.ErrBusy) {
		t.Fatalf("RunAccept with invalid manifest = %v, want a decode error", err)
	}
	if !strings.Contains(err.Error(), "decode") && !strings.Contains(err.Error(), "load") {
		t.Errorf("unexpected invalid-manifest error: %v", err)
	}
}

// TestRunUnmanageRootGitignoreRefused checks the cross-cutting .gitignore
// invariant at the core boundary.
func TestRunUnmanageRootGitignoreRefused(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("node_modules\n")
	writeManageFixture(t, root, map[string]string{".gitignore": string(pristine)})
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), pristine, 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	before := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName))

	err := RunUnmanage(manageRunContext(root), []string{".gitignore"})
	if err == nil || !strings.Contains(err.Error(), "root .gitignore cannot be unmanaged") {
		t.Fatalf("RunUnmanage error = %v", err)
	}
	if got := mustManageRead(t, filepath.Join(root, fileutil.ManifestFileName)); got != before {
		t.Errorf("refused unmanage rewrote the manifest")
	}
	if _, err := os.Stat(filepath.Join(root, tasks.BaseShadowRelPath(".gitignore"))); err != nil {
		t.Errorf("refused unmanage removed the root .gitignore base: %v", err)
	}
}

func mustManageRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled fixture path under t.TempDir()
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
