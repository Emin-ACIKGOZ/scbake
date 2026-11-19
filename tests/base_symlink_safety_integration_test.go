// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/util/fileutil"
)

// setupBaseSymlinkProject creates a project with the supplied new arguments and
// chdirs into it. The project root is a fresh temp directory.
func setupBaseSymlinkProject(t *testing.T, args ...string) string {
	t.Helper()

	work := t.TempDir()
	t.Chdir(work)

	out, err := runCLI(append([]string{"new", "app"}, args...)...)
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	project := filepath.Join(work, "app")
	t.Chdir(project)
	return project
}

// replaceBaseWithSymlink replaces the committed base directory with
// .scbake-base -> src and writes files under src. It returns the src directory.
func replaceBaseWithSymlink(t *testing.T, project string, files map[string]string) string {
	t.Helper()

	baseRoot := filepath.Join(project, fileutil.CommittedBaseDir)
	if err := os.RemoveAll(baseRoot); err != nil {
		t.Fatalf("remove committed base: %v", err)
	}
	srcDir := filepath.Join(project, "src")
	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatalf("create src: %v", err)
	}
	for name, content := range files {
		mustWrite(t, filepath.Join(srcDir, name), content)
	}
	if err := os.Symlink("src", baseRoot); err != nil {
		t.Skipf("symlink creation requires privileges: %v", err)
	}
	return srcDir
}

// requireSymlinkedBaseRefusal asserts the command failed with the fail-closed
// diagnostic and that every supplied file kept its local bytes.
func requireSymlinkedBaseRefusal(t *testing.T, out string, err error, command string, files map[string]string) {
	t.Helper()

	if err == nil {
		t.Fatalf("%s succeeded with a symlinked base:\n%s", command, out)
	}
	if !strings.Contains(out, "symlinked base directory is not allowed") {
		t.Errorf("%s did not explain the rejected symlinked base:\n%s", command, out)
	}
	if !strings.Contains(out, fileutil.CommittedBaseDir) {
		t.Errorf("%s did not name the offending %s path:\n%s", command, fileutil.CommittedBaseDir, out)
	}
	for path, want := range files {
		if got := mustRead(t, path); got != want {
			t.Fatalf("%s changed %s: got %q, want %q", command, path, got, want)
		}
	}
}

// TestSymlinkedBaseTreeFailsClosed is the #222 end-to-end guard: replacing the
// committed .scbake-base directory with an in-root symlink to src must make both
// apply (dry and real) and drift fail closed instead of writing through the link
// into src/Makefile.
func TestSymlinkedBaseTreeFailsClosed(t *testing.T) {
	project := setupBaseSymlinkProject(t, "--with", "makefile")

	local := "LOCAL WORK: keep me\n"
	srcDir := replaceBaseWithSymlink(t, project, map[string]string{"Makefile": local})
	makefile := filepath.Join(srcDir, "Makefile")

	for _, args := range [][]string{
		{"apply", "--with", "makefile", "--dry-run"},
		{"apply", "--with", "makefile"},
	} {
		out, err := runCLI(args...)
		requireSymlinkedBaseRefusal(t, out, err, strings.Join(args, " "), map[string]string{makefile: local})
	}

	out, err := runCLI("drift")
	requireSymlinkedBaseRefusal(t, out, err, "drift", map[string]string{makefile: local})
}

// TestAcceptSymlinkedBaseFailsClosed: accept must refuse a redirected base
// instead of reading pristine bytes through it and recording an accepted hash.
func TestAcceptSymlinkedBaseFailsClosed(t *testing.T) {
	project := setupBaseSymlinkProject(t, "--lang", "go")
	pristine := mustRead(t, filepath.Join(project, fileutil.CommittedBaseDir, "main.go"))
	srcDir := replaceBaseWithSymlink(t, project, map[string]string{"main.go": pristine})

	edited := "package main\n\nfunc main() { println(\"local\") }\n"
	mustWrite(t, filepath.Join(project, "main.go"), edited)
	manifestBefore := mustRead(t, filepath.Join(project, fileutil.ManifestFileName))

	out, err := runCLI("accept", "main.go")
	requireSymlinkedBaseRefusal(t, out, err, "accept", map[string]string{
		filepath.Join(srcDir, "main.go"):  pristine,
		filepath.Join(project, "main.go"): edited,
	})
	if got := mustRead(t, filepath.Join(project, fileutil.ManifestFileName)); got != manifestBefore {
		t.Fatalf("refused accept mutated scbake.toml")
	}
	if m := readAcceptManifest(t, project); len(m.AcceptedFiles) != 0 {
		t.Fatalf("refused accept recorded a baseline: %+v", m.AcceptedFiles)
	}

	out, err = runCLI("drift")
	requireSymlinkedBaseRefusal(t, out, err, "drift after refused accept", map[string]string{
		filepath.Join(srcDir, "main.go"): pristine,
	})
}

// TestUnmanageSymlinkedBaseFailsClosed is the B1 end-to-end guard: unmanage must
// not delete a user file through an in-root base symlink.
func TestUnmanageSymlinkedBaseFailsClosed(t *testing.T) {
	project := setupBaseSymlinkProject(t, "--with", "makefile")
	local := "LOCAL WORK: keep me\n"
	srcDir := replaceBaseWithSymlink(t, project, map[string]string{"Makefile": local})
	manifestBefore := mustRead(t, filepath.Join(project, fileutil.ManifestFileName))

	out, err := runCLI("unmanage", "Makefile")
	requireSymlinkedBaseRefusal(t, out, err, "unmanage", map[string]string{
		filepath.Join(srcDir, "Makefile"): local,
	})
	if got := mustRead(t, filepath.Join(project, fileutil.ManifestFileName)); got != manifestBefore {
		t.Fatalf("refused unmanage mutated scbake.toml")
	}
	info, statErr := os.Lstat(filepath.Join(project, fileutil.CommittedBaseDir))
	if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("refused unmanage changed the base root: mode %v (err %v)", info, statErr)
	}
}

// TestUnmanageRealBaseRemovesOnlyBase is the negative control: with a real base
// tree, unmanage still removes .scbake-base/Makefile and keeps the working file.
func TestUnmanageRealBaseRemovesOnlyBase(t *testing.T) {
	project := setupBaseSymlinkProject(t, "--with", "makefile")
	makefile := filepath.Join(project, "Makefile")
	edited := "LOCAL WORK: keep me\n"
	mustWrite(t, makefile, edited)
	basePath := filepath.Join(project, fileutil.CommittedBaseDir, "Makefile")

	out, err := runCLI("unmanage", "Makefile")
	requireAcceptSuccess(t, out, err, "unmanage")
	if got := mustRead(t, makefile); got != edited {
		t.Fatalf("unmanage rewrote the working file: %q", got)
	}
	if _, err := os.Stat(basePath); !os.IsNotExist(err) {
		t.Fatalf("unmanage did not remove the committed base: %v", err)
	}
	m := readAcceptManifest(t, project)
	if _, ok := m.ManagedFiles["Makefile"]; ok {
		t.Fatalf("unmanaged key stayed in managed_files")
	}
	if len(m.UnmanagedFiles) != 1 || m.UnmanagedFiles[0] != "Makefile" {
		t.Fatalf("unmanaged_files = %v, want [Makefile]", m.UnmanagedFiles)
	}
}

// TestUpdateSymlinkedBaseFailsClosed: update must fail closed before recreating
// a managed file or writing its pristine base through the link.
func TestUpdateSymlinkedBaseFailsClosed(t *testing.T) {
	project := setupBaseSymlinkProject(t, "--with", "makefile")
	local := "LOCAL WORK: keep me\n"
	srcDir := replaceBaseWithSymlink(t, project, map[string]string{"Makefile": local})
	if err := os.Remove(filepath.Join(project, "Makefile")); err != nil {
		t.Fatalf("remove working Makefile: %v", err)
	}

	out, err := runCLI("update")
	requireSymlinkedBaseRefusal(t, out, err, "update", map[string]string{
		filepath.Join(srcDir, "Makefile"): local,
	})
	if _, statErr := os.Stat(filepath.Join(project, "Makefile")); !os.IsNotExist(statErr) {
		t.Fatalf("failed update recreated the missing working file (stat err: %v)", statErr)
	}
}

// TestDriftRepairSymlinkedBaseFailsClosed: drift --repair must not repair
// through a redirected base tree.
func TestDriftRepairSymlinkedBaseFailsClosed(t *testing.T) {
	project := setupBaseSymlinkProject(t, "--with", "makefile")
	local := "LOCAL WORK: keep me\n"
	srcDir := replaceBaseWithSymlink(t, project, map[string]string{"Makefile": local})
	mustWrite(t, filepath.Join(project, "Makefile"), "user edit\n")

	out, err := runCLI("drift", "--repair")
	requireSymlinkedBaseRefusal(t, out, err, "drift --repair", map[string]string{
		filepath.Join(srcDir, "Makefile"): local,
	})
}
