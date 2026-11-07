// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// testZipEntry describes one archive member for writeTestZip.
type testZipEntry struct {
	name    string
	content string
	mode    os.FileMode
	dir     bool
}

// writeTestZip builds a zip fixture with archive/zip. Every entry is stored
// verbatim, including deliberately unsafe names.
func writeTestZip(t *testing.T, path string, entries ...testZipEntry) { //nolint:cyclop // fixture builder covers directories and verbatim names
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), fileutil.DirPerms); err != nil {
		t.Fatalf("create archive parent: %v", err)
	}
	f, err := os.Create(path) //nolint:gosec // test fixture inside t.TempDir
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range entries {
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		if entry.dir {
			mode = os.ModeDir | 0o755
		}
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		header.SetMode(mode)
		w, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatalf("create zip entry %q: %v", entry.name, err)
		}
		if entry.content != "" {
			if _, err := w.Write([]byte(entry.content)); err != nil {
				_ = zw.Close()
				_ = f.Close()
				t.Fatalf("write zip entry %q: %v", entry.name, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
}

func mustReadTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// assertTestFileMode compares permission bits exactly. Windows does not expose
// POSIX permission bits through os.Chmod, so only content is checked there.
func assertTestFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if got := info.Mode().Perm(); got != want.Perm() {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want.Perm())
	}
}

func newTestTaskContext(t *testing.T, root string) (types.TaskContext, *transaction.Manager) {
	t.Helper()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	return types.TaskContext{Ctx: context.Background(), TargetPath: root, Tx: tx}, tx
}

// TestTrackArchiveEntries_RestoresExistingNestedFile verifies that a file
// nested below an existing src/ directory is backed up before extraction and
// restored byte-for-byte, including its mode, after rollback.
func TestTrackArchiveEntries_RestoresExistingNestedFile(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "src", "main", "resources", "application.properties")
	const original = "custom.application.property=true\n"

	if err := os.MkdirAll(filepath.Dir(nested), fileutil.DirPerms); err != nil {
		t.Fatalf("create nested dirs: %v", err)
	}
	if err := os.WriteFile(nested, []byte(original), 0o640); err != nil { //nolint:gosec // mode restoration under test
		t.Fatalf("seed nested file: %v", err)
	}
	if err := os.Chmod(nested, 0o640); err != nil { //nolint:gosec // mode restoration under test
		t.Fatalf("set nested mode: %v", err)
	}

	writeTestZip(t, filepath.Join(root, "dist", "spring.zip"), testZipEntry{
		name:    "src/main/resources/application.properties",
		content: "spring.application.name=generated\n",
		mode:    0o644,
	})

	tc, tx := newTestTaskContext(t, root)
	if err := trackArchiveEntries(tc, root, filepath.Join("dist", "spring.zip")); err != nil {
		t.Fatalf("trackArchiveEntries: %v", err)
	}

	// Simulate what unzip -o does after tracking: overwrite content and mode.
	if err := os.WriteFile(nested, []byte("spring.application.name=generated\n"), 0o600); err != nil {
		t.Fatalf("simulate extraction: %v", err)
	}
	if err := os.Chmod(nested, 0o755); err != nil { //nolint:gosec // extraction mode simulation under test
		t.Fatalf("simulate extraction mode: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := mustReadTestFile(t, nested); got != original {
		t.Fatalf("nested file content after rollback = %q, want %q", got, original)
	}
	assertTestFileMode(t, nested, 0o640)
}

// TestTrackArchiveEntries_RemovesCreatedNestedPaths verifies that missing
// archive destinations and their missing ancestors are removed on rollback.
func TestTrackArchiveEntries_RemovesCreatedNestedPaths(t *testing.T) { //nolint:cyclop // two rollback scenarios with explicit setup
	t.Run("missing src is removed with its ancestors", func(t *testing.T) {
		root := t.TempDir()
		writeTestZip(t, filepath.Join(root, "dist", "new.zip"),
			testZipEntry{name: "src/new/deep/file.txt", content: "one\n"},
			testZipEntry{name: "src/new/deep/", dir: true},
		)

		tc, tx := newTestTaskContext(t, root)
		if err := trackArchiveEntries(tc, root, filepath.Join("dist", "new.zip")); err != nil {
			t.Fatalf("trackArchiveEntries: %v", err)
		}

		// Simulate extraction creating every missing directory.
		if err := os.MkdirAll(filepath.Join(root, "src", "new", "deep"), fileutil.DirPerms); err != nil {
			t.Fatalf("simulate extraction dirs: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "src", "new", "deep", "file.txt"), []byte("one\n"), 0o600); err != nil {
			t.Fatalf("simulate extraction file: %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "src")); !os.IsNotExist(err) {
			t.Fatalf("created src tree survived rollback; stat err = %v", err)
		}
	})

	t.Run("existing src survives while new subtree is removed", func(t *testing.T) {
		root := t.TempDir()
		keep := filepath.Join(root, "src", "keep.txt")
		if err := os.MkdirAll(filepath.Dir(keep), fileutil.DirPerms); err != nil {
			t.Fatalf("create existing src: %v", err)
		}
		if err := os.WriteFile(keep, []byte("keep\n"), 0o600); err != nil {
			t.Fatalf("seed existing src: %v", err)
		}

		writeTestZip(t, filepath.Join(root, "dist", "new.zip"), testZipEntry{
			name:    "src/new/deep/file.txt",
			content: "one\n",
		})

		tc, tx := newTestTaskContext(t, root)
		if err := trackArchiveEntries(tc, root, filepath.Join("dist", "new.zip")); err != nil {
			t.Fatalf("trackArchiveEntries: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "src", "new", "deep"), fileutil.DirPerms); err != nil {
			t.Fatalf("simulate extraction dirs: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "src", "new", "deep", "file.txt"), []byte("one\n"), 0o600); err != nil {
			t.Fatalf("simulate extraction file: %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "src", "new")); !os.IsNotExist(err) {
			t.Fatalf("created subtree survived rollback; stat err = %v", err)
		}
		if got := mustReadTestFile(t, keep); got != "keep\n" {
			t.Fatalf("pre-existing file was disturbed: %q", got)
		}
	})
}

// TestTrackArchiveEntries_RestoresMvnwMode verifies that a mode-only change
// (0644 -> 0755) to an extracted file is restored by rollback.
func TestTrackArchiveEntries_RestoresMvnwMode(t *testing.T) {
	root := t.TempDir()
	mvnw := filepath.Join(root, "mvnw")
	const content = "#!/bin/sh\necho maven\n"

	if err := os.WriteFile(mvnw, []byte(content), 0o644); err != nil { //nolint:gosec // mode restoration under test
		t.Fatalf("seed mvnw: %v", err)
	}
	if err := os.Chmod(mvnw, 0o644); err != nil { //nolint:gosec // mode restoration under test
		t.Fatalf("set mvnw mode: %v", err)
	}

	writeTestZip(t, filepath.Join(root, "dist", "spring.zip"), testZipEntry{
		name:    "mvnw",
		content: content,
		mode:    0o755,
	})

	tc, tx := newTestTaskContext(t, root)
	if err := trackArchiveEntries(tc, root, filepath.Join("dist", "spring.zip")); err != nil {
		t.Fatalf("trackArchiveEntries: %v", err)
	}
	if err := os.Chmod(mvnw, 0o755); err != nil { //nolint:gosec // chmod +x simulation under test
		t.Fatalf("simulate chmod +x: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := mustReadTestFile(t, mvnw); got != content {
		t.Fatalf("mvnw content after rollback = %q, want %q", got, content)
	}
	assertTestFileMode(t, mvnw, 0o644)
}

// TestSafeArchiveEntryName_RejectsDrivePrefixAfterCleaning pins a defect found
// by FuzzSafeArchiveEntryName: the drive-prefix check runs before path.Clean,
// so "./A:" is accepted and normalized to the drive-relative path "A:". On
// Windows that result is not a local relative path, which violates the
// validator's stated cross-platform contract. This test intentionally fails
// until the validator re-checks the cleaned name; it must not be weakened.
func TestSafeArchiveEntryName_RejectsDrivePrefixAfterCleaning(t *testing.T) {
	for _, name := range []string{"./A:", "./C:/escape.txt", ".//C:/escape.txt"} {
		t.Run(name, func(t *testing.T) {
			if got, err := safeArchiveEntryName(name); err == nil {
				t.Fatalf("safeArchiveEntryName(%q) = %q, nil; want a drive-prefix error", name, got)
			}
		})
	}
}

// TestExecCommandTask_ArchiveRejectsUnsafeEntryNames verifies that unsafe
// archive names fail before any path is tracked and before the command runs.
func TestExecCommandTask_ArchiveRejectsUnsafeEntryNames(t *testing.T) {
	cases := []struct {
		name  string
		entry string
	}{
		{"absolute", "/etc/passwd"},
		{"traversal", "../escape.txt"},
		{"nested traversal", "nested/../../escape.txt"},
		{"bare parent", ".."},
		{"empty", ""},
		{"dot", "."},
		{"backslash", `nested\escape.txt`},
		{"windows drive", "C:escape.txt"},
		{"windows drive slash", "C:/escape.txt"},
		{"windows drive backslash", `C:\escape.txt`},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestZip(t, filepath.Join(root, "unsafe.zip"), testZipEntry{
				name:    tt.entry,
				content: "payload",
			})
			assertArchiveTaskFailsClosed(t, root, "unsafe.zip")
		})
	}
}

// TestExecCommandTask_ArchiveFailsClosed verifies that an archive outside the
// transaction root, a missing archive, or an invalid archive prevents the
// extraction command from running.
func TestExecCommandTask_ArchiveFailsClosed(t *testing.T) {
	t.Run("archive outside root", func(t *testing.T) {
		base := t.TempDir()
		outsideZip := filepath.Join(base, "outside.zip")
		writeTestZip(t, outsideZip, testZipEntry{name: "ok.txt", content: "payload"})

		root := filepath.Join(base, "root")
		if err := os.Mkdir(root, fileutil.DirPerms); err != nil {
			t.Fatalf("create root: %v", err)
		}
		assertArchiveTaskFailsClosed(t, root, outsideZip)
	})

	t.Run("relative archive path outside root", func(t *testing.T) {
		base := t.TempDir()
		writeTestZip(t, filepath.Join(base, "outside.zip"), testZipEntry{name: "ok.txt", content: "payload"})

		root := filepath.Join(base, "root")
		if err := os.Mkdir(root, fileutil.DirPerms); err != nil {
			t.Fatalf("create root: %v", err)
		}
		assertArchiveTaskFailsClosed(t, root, filepath.Join("..", "outside.zip"))
	})

	t.Run("missing archive", func(t *testing.T) {
		assertArchiveTaskFailsClosed(t, t.TempDir(), "missing.zip")
	})

	t.Run("invalid archive", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "bad.zip"), []byte("not a zip file\n"), 0o600); err != nil {
			t.Fatalf("seed invalid archive: %v", err)
		}
		assertArchiveTaskFailsClosed(t, root, "bad.zip")
	})
}

// assertArchiveTaskFailsClosed runs an ArchivePath extraction whose tracking
// must fail, then proves the command never ran and nothing was tracked.
func assertArchiveTaskFailsClosed(t *testing.T, root, archivePath string) {
	t.Helper()

	sentinel := filepath.Join(t.TempDir(), "command-ran")
	t.Setenv("GO_WANT_EXEC_HELPER", "sentinel")
	t.Setenv("GO_WANT_EXEC_HELPER_PATH", sentinel)

	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	task := &ExecCommandTask{
		Cmd:         os.Args[0],
		Args:        []string{"-test.run=^TestExecCommandTaskHelper$"},
		Desc:        "Must not run",
		RunInTarget: true,
		ArchivePath: archivePath,
	}
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: root, Tx: tx}

	if err := task.Execute(tc); err == nil {
		t.Fatal("expected archive tracking to fail")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("extraction command ran despite a tracking failure; stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir)); !os.IsNotExist(err) {
		t.Fatalf("unsafe archive was tracked before validation; stat err = %v", err)
	}
}

// TestExecCommandTask_ArchivePathUsesCommandWorkingDir verifies relative
// archive paths resolve against the same directory as the command, both for
// RunInTarget and for the process working directory.
func TestExecCommandTask_ArchivePathUsesCommandWorkingDir(t *testing.T) { //nolint:cyclop // two command-directory scenarios
	t.Run("run in target subdirectory", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "app")
		if err := os.Mkdir(target, fileutil.DirPerms); err != nil {
			t.Fatalf("create target: %v", err)
		}
		writeTestZip(t, filepath.Join(target, "dist", "app.zip"), testZipEntry{
			name:    "src/nested.txt",
			content: "archive\n",
		})

		extracted := filepath.Join(target, "src", "nested.txt")
		t.Setenv("GO_WANT_EXEC_HELPER", "mutate")
		t.Setenv("GO_WANT_EXEC_HELPER_PATH", extracted)
		t.Setenv("GO_WANT_EXEC_HELPER_CONTENT", "extracted\n")

		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New: %v", err)
		}
		task := &ExecCommandTask{
			Cmd:         os.Args[0],
			Args:        []string{"-test.run=^TestExecCommandTaskHelper$"},
			Desc:        "Extract into target",
			RunInTarget: true,
			ArchivePath: filepath.Join("dist", "app.zip"),
		}
		tc := types.TaskContext{Ctx: context.Background(), TargetPath: target, Tx: tx}

		if err := task.Execute(tc); err != nil {
			t.Fatalf("task failed: %v", err)
		}
		if got := mustReadTestFile(t, extracted); got != "extracted\n" {
			t.Fatalf("extracted content = %q", got)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "src")); !os.IsNotExist(err) {
			t.Fatalf("created extraction tree survived rollback; stat err = %v", err)
		}
	})

	t.Run("run in process working directory", func(t *testing.T) {
		root := t.TempDir()
		writeTestZip(t, filepath.Join(root, "dist", "app.zip"), testZipEntry{
			name:    "generated/nested.txt",
			content: "archive\n",
		})

		extracted := filepath.Join(root, "generated", "nested.txt")
		t.Setenv("GO_WANT_EXEC_HELPER", "mutate")
		t.Setenv("GO_WANT_EXEC_HELPER_PATH", extracted)
		t.Setenv("GO_WANT_EXEC_HELPER_CONTENT", "extracted\n")

		t.Chdir(root)

		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New: %v", err)
		}
		task := &ExecCommandTask{
			Cmd:         os.Args[0],
			Args:        []string{"-test.run=^TestExecCommandTaskHelper$"},
			Desc:        "Extract in current directory",
			RunInTarget: false,
			ArchivePath: filepath.Join("dist", "app.zip"),
		}
		tc := types.TaskContext{Ctx: context.Background(), TargetPath: root, Tx: tx}

		if err := task.Execute(tc); err != nil {
			t.Fatalf("task failed: %v", err)
		}
		if got := mustReadTestFile(t, extracted); got != "extracted\n" {
			t.Fatalf("extracted content = %q", got)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "generated")); !os.IsNotExist(err) {
			t.Fatalf("created extraction tree survived rollback; stat err = %v", err)
		}
	})
}
