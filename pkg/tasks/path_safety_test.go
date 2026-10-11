// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// requireSymlinks skips tests that require unprivileged symlink creation.
func requireSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
}

// setupEscapeRoot returns a project root containing a symlink named "escape"
// that points at a directory outside the root.
func setupEscapeRoot(t *testing.T) (root, outside string) {
	t.Helper()
	root = t.TempDir()
	outside = t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return root, outside
}

// TestAppendFileTask_SymlinkDirectoryEscapeRejected is a regression test for
// SCB-001 at the AppendFileTask sink.
func TestAppendFileTask_SymlinkDirectoryEscapeRejected(t *testing.T) {
	requireSymlinks(t)
	root, outside := setupEscapeRoot(t)

	task := &AppendFileTask{
		FilePath: filepath.Join("escape", "pwned.txt"),
		Content:  "unauthorized",
		Desc:     "escape symlink",
		TaskPrio: 100,
	}
	tc := types.TaskContext{TargetPath: root}

	if err := task.Execute(tc); err == nil {
		t.Error("AppendFileTask accepted an escaping symlinked directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); !os.IsNotExist(err) {
		t.Error("AppendFileTask created a file outside the root")
	}
}

// TestAppendFileTask_SymlinkLeafRejected verifies a symlinked final component is
// rejected and its target is not modified.
func TestAppendFileTask_SymlinkLeafRejected(t *testing.T) {
	requireSymlinks(t)
	root := t.TempDir()
	outside := t.TempDir()

	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("original"), fileutil.FilePerms); err != nil {
		t.Fatalf("seed victim: %v", err)
	}
	if err := os.Symlink(victim, filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	task := &AppendFileTask{FilePath: "link.txt", Content: "pwned", Desc: "leaf symlink", TaskPrio: 100}
	tc := types.TaskContext{TargetPath: root}

	if err := task.Execute(tc); err == nil {
		t.Error("AppendFileTask followed a symlinked leaf")
	}

	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(got) != "original" {
		t.Errorf("symlink target was modified: %q", got)
	}
}

// TestCreateDirTask_SymlinkDirectoryEscapeRejected is a regression test for
// SCB-001 at the CreateDirTask sink.
func TestCreateDirTask_SymlinkDirectoryEscapeRejected(t *testing.T) {
	requireSymlinks(t)
	root, outside := setupEscapeRoot(t)

	task := &CreateDirTask{
		Path:     filepath.Join("escape", "sub"),
		Desc:     "escape symlink",
		TaskPrio: 50,
	}
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: root}

	if err := task.Execute(tc); err == nil {
		t.Error("CreateDirTask accepted an escaping symlinked directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "sub")); !os.IsNotExist(err) {
		t.Error("CreateDirTask created a directory outside the root")
	}
}

// TestInsertXMLTask_SymlinkDirectoryEscapeRejected is a regression test for
// SCB-001 at the InsertXMLTask sink.
func TestInsertXMLTask_SymlinkDirectoryEscapeRejected(t *testing.T) {
	requireSymlinks(t)
	root, outside := setupEscapeRoot(t)

	xml := []byte(`<project><build><plugins>existing</plugins></build></project>`)
	outFile := filepath.Join(outside, "pom.xml")
	if err := os.WriteFile(outFile, xml, fileutil.FilePerms); err != nil {
		t.Fatalf("seed outside xml: %v", err)
	}

	task := &InsertXMLTask{
		FilePath:    filepath.Join("escape", "pom.xml"),
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin><groupId>evil</groupId></plugin>`,
		Desc:        "escape symlink",
		TaskPrio:    1201,
	}
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: root, Manifest: &types.Manifest{}}

	if err := task.Execute(tc); err == nil {
		t.Error("InsertXMLTask accepted an escaping symlinked directory")
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read outside xml: %v", err)
	}
	if string(got) != string(xml) {
		t.Errorf("XML outside the root was modified: %q", got)
	}
}

// TestInsertXMLTask_SymlinkLeafRejected verifies a symlinked final component is
// rejected and its target is not modified.
func TestInsertXMLTask_SymlinkLeafRejected(t *testing.T) {
	requireSymlinks(t)
	root := t.TempDir()
	outside := t.TempDir()

	xml := []byte(`<project><build><plugins>existing</plugins></build></project>`)
	outFile := filepath.Join(outside, "pom.xml")
	if err := os.WriteFile(outFile, xml, fileutil.FilePerms); err != nil {
		t.Fatalf("seed outside xml: %v", err)
	}
	if err := os.Symlink(outFile, filepath.Join(root, "pom.xml")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin><groupId>evil</groupId></plugin>`,
		Desc:        "leaf symlink",
		TaskPrio:    1201,
	}
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: root, Manifest: &types.Manifest{}}

	if err := task.Execute(tc); err == nil {
		t.Error("InsertXMLTask followed a symlinked leaf")
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read outside xml: %v", err)
	}
	if string(got) != string(xml) {
		t.Errorf("XML outside the root was modified: %q", got)
	}
}

// TestWriteManagedFile_SymlinkDirectoryEscapeRejected is a regression test for
// SCB-001 at the WriteManagedFile sink used by CreateTemplateTask.
func TestWriteManagedFile_SymlinkDirectoryEscapeRejected(t *testing.T) {
	requireSymlinks(t)
	root, outside := setupEscapeRoot(t)

	tc := types.TaskContext{Ctx: context.Background(), TargetPath: root, Manifest: &types.Manifest{}}
	if err := WriteManagedFile(tc, filepath.Join("escape", "pwned.txt"), []byte("x"), fileutil.FilePerms); err == nil {
		t.Error("WriteManagedFile accepted an escaping symlinked directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); !os.IsNotExist(err) {
		t.Error("WriteManagedFile created a file outside the root")
	}
}

// TestCreateTemplateTask_SymlinkDirectoryEscapeRejected exercises the
// CreateTemplateTask sink end to end.
func TestCreateTemplateTask_SymlinkDirectoryEscapeRejected(t *testing.T) {
	requireSymlinks(t)
	root, outside := setupEscapeRoot(t)

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   filepath.Join("escape", "out.txt"),
		Desc:         "escape symlink",
		TaskPrio:     100,
	}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: root,
		Manifest:   &types.Manifest{Projects: []types.Project{{Name: "x"}}},
	}

	if err := task.Execute(tc); err == nil {
		t.Error("CreateTemplateTask accepted an escaping symlinked directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "out.txt")); !os.IsNotExist(err) {
		t.Error("CreateTemplateTask created a file outside the root")
	}
}

// TestAppendFileTask_InRootSymlinkAllowed verifies that a symlinked directory
// which resolves back inside the root remains usable.
func TestAppendFileTask_InRootSymlinkAllowed(t *testing.T) {
	requireSymlinks(t)
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, fileutil.DirPerms); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	task := &AppendFileTask{
		FilePath: filepath.Join("link", "created.txt"),
		Content:  "hello",
		Desc:     "in-root symlink",
		TaskPrio: 100,
	}
	tc := types.TaskContext{TargetPath: root}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("in-root symlinked directory was rejected: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(real, "created.txt"))
	if err != nil {
		t.Fatalf("file was not created under the resolved directory: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}
