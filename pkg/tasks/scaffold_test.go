// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
)

const scaffoldHelperMarker = "scaffold-helper-mode="

// TestScaffoldHelperProcess is not a real test. It is the fake scaffolder used
// by the ScaffoldTask tests: the test binary re-executes itself with a mode in
// its arguments so the tests do not depend on a shell or an external tool.
//
//nolint:cyclop // fixture dispatch: one branch per scaffolding behavior under test
func TestScaffoldHelperProcess(t *testing.T) {
	mode := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, scaffoldHelperMarker) {
			mode = strings.TrimPrefix(arg, scaffoldHelperMarker)
			break
		}
	}
	if mode == "" {
		t.Skip("helper process only")
	}

	write := func(name, content string) {
		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			os.Exit(2)
		}
	}

	switch mode {
	case "success":
		name := filepath.Base(mustGetwd())
		write("package.json", `{"name":"`+name+`"}`+"\n")
		write("index.html", "<h1>"+name+"</h1>\n")
		write(".gitignore", "node_modules\n")
		write("svelte.config.js", "export default {}\n")
		if err := os.MkdirAll(filepath.Join("src", "lib"), 0o750); err != nil {
			os.Exit(2)
		}
		write(filepath.Join("src", "main.js"), "console.log('"+name+"')\n")
		if err := os.MkdirAll("public", 0o750); err != nil {
			os.Exit(2)
		}
		write(filepath.Join("public", "favicon.svg"), "<svg/>\n")
	case "sentinel-missing":
		write("README.md", "canceled\n")
	case "collision":
		name := filepath.Base(mustGetwd())
		write("package.json", `{"name":"`+name+`"}`+"\n")
		write("index.html", "<h1>scaffold</h1>\n")
	case "cleanup-fail":
		name := filepath.Base(mustGetwd())
		write("package.json", `{"name":"`+name+`"}`+"\n")
		write("index.html", "<h1>"+name+"</h1>\n")
		// Write a sibling of the staging leaf so the post-move RemoveAll of the
		// staging root cannot complete. This models an open handle or an
		// undeletable file left by the scaffolder.
		locked := filepath.Join("..", "locked")
		if err := os.MkdirAll(locked, 0o750); err != nil {
			os.Exit(2)
		}
		write(filepath.Join(locked, "keep.txt"), "locked\n")
		if err := os.Chmod(locked, 0o500); err != nil { //nolint:gosec // fixture needs an undeletable staging sibling
			os.Exit(2)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		os.Exit(2)
	}
	return wd
}

func newScaffoldContext(t *testing.T, root, target string) types.TaskContext {
	t.Helper()
	m, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	t.Cleanup(func() { _ = m.Rollback() })
	return types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: target,
		Tx:         m,
	}
}

func newScaffoldTask(mode string) *ScaffoldTask {
	return &ScaffoldTask{
		Cmd:      os.Args[0],
		Args:     []string{"-test.run=^TestScaffoldHelperProcess$", scaffoldHelperMarker + mode},
		Desc:     "fake scaffold",
		TaskPrio: 100,
		Sentinel: "package.json",
	}
}

func readPackageName(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return pkg.Name
}

// TestScaffoldTaskMovesOutputAndPreservesName verifies the staging move and
// that the package name still derives from the target basename, not the
// staging directory name.
func TestScaffoldTaskMovesOutputAndPreservesName(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "web")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("creating target: %v", err)
	}

	tc := newScaffoldContext(t, root, target)
	if err := newScaffoldTask("success").Execute(tc); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := tc.Tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if got := readPackageName(t, filepath.Join(target, "package.json")); got != "web" {
		t.Errorf("package name = %q, want %q", got, "web")
	}
	for _, rel := range []string{"index.html", ".gitignore", "svelte.config.js", filepath.Join("src", "main.js"), filepath.Join("public", "favicon.svg")} {
		if _, err := os.Stat(filepath.Join(target, rel)); err != nil {
			t.Errorf("moved output missing %s: %v", rel, err)
		}
	}
	assertNoStagingDirs(t, root)
}

// TestScaffoldTaskRollbackRemovesMovedOutput verifies that a later task
// failure, simulated by rolling the transaction back after the move, removes
// the moved files and directories.
func TestScaffoldTaskRollbackRemovesMovedOutput(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "web")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("creating target: %v", err)
	}

	m, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: target, Tx: m}
	if err := newScaffoldTask("success").Execute(tc); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "package.json")); err != nil {
		t.Fatalf("scaffold did not move output before rollback: %v", err)
	}

	if err := m.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("reading target after rollback: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("rollback left %d entries in target: %v", len(entries), entries)
	}
	if _, err := os.Stat(filepath.Join(root, ".scbake")); !os.IsNotExist(err) {
		t.Errorf("rollback left transaction state: %v", err)
	}
}

// TestScaffoldTaskSentinelFailureWritesNothing verifies that a scaffolder which
// exits 0 without producing package.json is treated as a failure and that no
// staging output leaks into the target.
func TestScaffoldTaskSentinelFailureWritesNothing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "sv")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("creating target: %v", err)
	}

	err := newScaffoldTask("sentinel-missing").Execute(newScaffoldContext(t, root, target))
	if err == nil {
		t.Fatal("expected sentinel failure, got nil")
	}
	entries, readErr := os.ReadDir(target)
	if readErr != nil {
		t.Fatalf("reading target: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("sentinel failure wrote into target: %v", entries)
	}
}

// TestScaffoldTaskCollisionPreservesUserFile verifies the preflight failure:
// a pre-existing destination is neither overwritten nor accompanied by a
// partially moved scaffold.
func TestScaffoldTaskCollisionPreservesUserFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "web")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("creating target: %v", err)
	}
	const userContent = "USER CONTENT\n"
	if err := os.WriteFile(filepath.Join(target, "index.html"), []byte(userContent), 0o600); err != nil {
		t.Fatalf("writing user file: %v", err)
	}

	err := newScaffoldTask("collision").Execute(newScaffoldContext(t, root, target))
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}

	got, readErr := os.ReadFile(filepath.Join(target, "index.html")) //nolint:gosec // test-controlled path
	if readErr != nil {
		t.Fatalf("reading user file: %v", readErr)
	}
	if string(got) != userContent {
		t.Errorf("user file = %q, want %q", got, userContent)
	}
	entries, readErr := os.ReadDir(target)
	if readErr != nil {
		t.Fatalf("reading target: %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "index.html" {
		t.Errorf("collision moved other scaffold entries: %v", entries)
	}
}

// stageMoveFixture creates a target and a populated staging leaf and returns
// the staged top-level entries, mirroring what Execute sees after the
// scaffolder runs.
func stageMoveFixture(t *testing.T, root string) (target, stagingLeaf string, entries []os.DirEntry) {
	t.Helper()
	target = filepath.Join(root, "web")
	stagingLeaf = filepath.Join(root, "stage", "web")
	for _, dir := range []string{target, stagingLeaf} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}
	for name, content := range map[string]string{
		"index.html":   "SCAFFOLD\n",
		"package.json": `{"name":"web"}` + "\n",
	} {
		if err := os.WriteFile(filepath.Join(stagingLeaf, name), []byte(content), 0o600); err != nil {
			t.Fatalf("writing staged %s: %v", name, err)
		}
	}
	entries, err := os.ReadDir(stagingLeaf)
	if err != nil {
		t.Fatalf("reading staging leaf: %v", err)
	}
	return target, stagingLeaf, entries
}

// TestMoveScaffoldEntriesRechecksDestination covers MF2 deterministically: a
// destination created after the preflight (the preflight-to-rename window) is
// rejected by the per-entry recheck instead of being overwritten untracked.
func TestMoveScaffoldEntriesRechecksDestination(t *testing.T) {
	target, stagingLeaf, entries := stageMoveFixture(t, t.TempDir())

	// This is the state Execute sees at preflight: no destination exists yet.
	if err := preflightDestinations(target, entries); err != nil {
		t.Fatalf("preflight before the simulated race: %v", err)
	}

	// Simulate a concurrent writer landing the first destination in the window
	// between preflight and rename.
	const userContent = "USER CONTENT\n"
	userFile := filepath.Join(target, "index.html")
	if err := os.WriteFile(userFile, []byte(userContent), 0o600); err != nil {
		t.Fatalf("writing late destination: %v", err)
	}

	err := moveScaffoldEntries(stagingLeaf, target, entries)
	if err == nil {
		t.Fatal("expected the per-entry destination recheck to fail")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("recheck error = %v, want an existing-destination error", err)
	}
	got, readErr := os.ReadFile(userFile) //nolint:gosec // test-controlled path
	if readErr != nil {
		t.Fatalf("reading user file: %v", readErr)
	}
	if string(got) != userContent {
		t.Errorf("user file = %q, want %q", got, userContent)
	}
	if _, statErr := os.Stat(filepath.Join(target, "package.json")); !os.IsNotExist(statErr) {
		t.Errorf("recheck failure still moved another entry: %v", statErr)
	}
}

// TestScaffoldTaskRejectsEscapingTargetSymlink verifies that the lexical-target
// fix preserves the security boundary: a target that is itself a symlink
// resolving outside the transaction root is rejected before anything moves.
func TestScaffoldTaskRejectsEscapingTargetSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires additional privileges on Windows")
	}

	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(root, "web")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatalf("creating escaping target symlink: %v", err)
	}

	err := newScaffoldTask("success").Execute(newScaffoldContext(t, root, target))
	if err == nil {
		t.Fatal("expected an escaping target symlink to be rejected")
	}
	if !strings.Contains(err.Error(), "security violation") || !strings.Contains(err.Error(), "resolves outside") {
		t.Errorf("error = %v, want a symlink containment security violation", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatalf("reading outside directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("escaping target received scaffold output: %v", entries)
	}
}

// TestScaffoldTaskDryRunWritesNothing verifies that dry-run returns before any
// staging directory or transaction journal is created.
func TestScaffoldTaskDryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "sv")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("creating target: %v", err)
	}

	tc := types.TaskContext{Ctx: context.Background(), TargetPath: target, DryRun: true}
	if err := newScaffoldTask("success").Execute(tc); err != nil {
		t.Fatalf("dry-run Execute: %v", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("reading target: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dry-run wrote into target: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(root, ".scbake")); !os.IsNotExist(err) {
		t.Errorf("dry-run created transaction state: %v", err)
	}
}

// TestScaffoldTaskCleanupFailureIsNotFatal verifies MF1: after a successful
// move, an undeletable staging tree only warns; it must not turn the completed
// scaffold into a task error.
//
// The fixture uses directory permissions to force the failure. On systems that
// allow the owner to remove such a tree anyway (for example when tests run as
// root), cleanup succeeds and the test passes without exercising the warning
// path. The production guarantee is documented on scaffoldStaging.remove and
// at the deferred call in ScaffoldTask.Execute.
func TestScaffoldTaskCleanupFailureIsNotFatal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission bits do not block removal on Windows")
	}

	root := t.TempDir()
	target := filepath.Join(root, "sv")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("creating target: %v", err)
	}

	tc := newScaffoldContext(t, root, target)
	if err := newScaffoldTask("cleanup-fail").Execute(tc); err != nil {
		t.Fatalf("cleanup failure became a task error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "package.json")); err != nil {
		t.Errorf("completed scaffold missing after cleanup failure: %v", err)
	}
	if got := readPackageName(t, filepath.Join(target, "package.json")); got != "sv" {
		t.Errorf("package name = %q, want sv", got)
	}

	// Make the tree removable again so the test cleanup and rollback can finish.
	scbakeRoot := filepath.Join(root, ".scbake")
	_ = filepath.WalkDir(scbakeRoot, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(path, 0o700) //nolint:gosec // restore fixture permissions for cleanup
		}
		return nil
	})
	if err := tc.Tx.Rollback(); err != nil {
		t.Fatalf("Rollback after permission restore: %v", err)
	}
}

// TestScaffoldTaskRejectsFilesystemRoot verifies that staging can never be
// created at the filesystem root with a nonsensical empty basename.
func TestScaffoldTaskRejectsFilesystemRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filesystem root differs on Windows")
	}
	root := t.TempDir()
	tc := newScaffoldContext(t, root, "/")
	if err := newScaffoldTask("success").Execute(tc); err == nil {
		t.Fatal("expected filesystem-root target to be rejected")
	}
}

// assertNoStagingDirs walks root and fails if any stage-* or scaffold-* directory
// survived a successful task.
func assertNoStagingDirs(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() && (strings.HasPrefix(name, "stage-") || strings.HasPrefix(name, "scaffold-")) {
			t.Errorf("staging residue left behind: %s", path)
		}
		if entry.IsDir() && name == ".scbake" {
			t.Errorf("transaction state survived a successful task: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}
