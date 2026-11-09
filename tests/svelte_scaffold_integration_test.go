// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeCreateViteNpm stands in for create-vite: a non-empty working directory
// cancels with exit 0 and writes nothing; an empty directory gets the Svelte
// template. `npm install` requires package.json, and `npm pkg set` succeeds.
// The fixture is a POSIX shell script, so these tests skip on Windows (./tests
// does not run there in CI).
const fakeCreateViteNpm = `#!/bin/sh
set -eu

case "${1:-}" in
create)
	entries=$(ls -A)
	if [ -n "$entries" ]; then
		echo "Operation canceled"
		exit 0
	fi
	name=$(basename "$PWD")
	printf '{"name":"%s","private":true,"type":"module"}\n' "$name" > package.json
	mkdir -p src public
	printf '<h1>%s</h1>\n' "$name" > index.html
	printf 'node_modules\ndist\n' > .gitignore
	printf 'export default {}\n' > svelte.config.js
	printf 'export default {}\n' > vite.config.js
	;;
install)
	if [ ! -f package.json ]; then
		echo "npm ERR! enoent ENOENT: no such file or directory, open 'package.json'" >&2
		exit 1
	fi
	printf '{}\n' > package-lock.json
	mkdir -p node_modules
	: > node_modules/.package-lock.json
	;;
pkg)
	exit 0
	;;
*)
	echo "fake npm: unsupported invocation: $*" >&2
	exit 1
	;;
esac
`

const svelteIssue136WantName = "sv"

// TestSvelteNewScaffoldsInNonEmptyTarget is the offline regression test for
// issue #136. By the time the language handler runs, the target already
// contains scbake.toml and .scbake; before the fix create-vite saw a non-empty
// directory, canceled with exit 0, and the following npm install failed with
// ENOENT.
func TestSvelteNewScaffoldsInNonEmptyTarget(t *testing.T) {
	skipUnlessShellFixture(t)

	work := t.TempDir()
	binDir := installFakeNpm(t, work)

	output, err := runScbake(t, work, binDir, "new", svelteIssue136WantName, "--lang", "svelte")
	if err != nil {
		t.Fatalf("new --lang svelte failed: %v\n%s", err, output)
	}

	project := filepath.Join(work, svelteIssue136WantName)
	assertScaffoldedSvelteProject(t, project, svelteIssue136WantName, ".")
	assertNoScaffoldResidue(t, work)
}

// TestSvelteApplyStandaloneEmptyTarget verifies apply parity for a fresh
// standalone target, which before the fix failed for the same reason as `new`.
func TestSvelteApplyStandaloneEmptyTarget(t *testing.T) {
	skipUnlessShellFixture(t)

	work := t.TempDir()
	binDir := installFakeNpm(t, work)
	target := filepath.Join(work, "empty")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("creating empty target: %v", err)
	}

	output, err := runScbake(t, work, binDir, "apply", "--lang", "svelte", "empty")
	if err != nil {
		t.Fatalf("apply --lang svelte empty failed: %v\n%s", err, output)
	}

	assertScaffoldedSvelteProject(t, target, "empty", "empty")
	if _, err := os.Stat(filepath.Join(work, "scbake.toml")); !os.IsNotExist(err) {
		t.Errorf("standalone apply wrote a repository-root manifest: %v", err)
	}
	assertNoScaffoldResidue(t, work)
}

// TestSvelteApplySymlinkedRepoRoot is the end-to-end regression for the
// symlinked-root regression: the repository root is reached through link ->
// real and apply must scaffold apps/web without the transaction rejecting the
// resolved destinations as escaping the lexical root.
func TestSvelteApplySymlinkedRepoRoot(t *testing.T) {
	skipUnlessShellFixture(t)

	work := t.TempDir()
	binDir := installFakeNpm(t, work)
	link, realDir := setupSymlinkedRepo(t, work)

	output, err := runScbake(t, link, binDir, "apply", "--lang", "svelte", "apps/web")
	if err != nil {
		t.Fatalf("apply --lang svelte apps/web through a symlinked root failed: %v\n%s", err, output)
	}

	target := filepath.Join(realDir, "apps", "web")
	assertScaffoldedSvelteFiles(t, target, "web")
	assertManifestProject(t, realDir, "apps/web", "web")
	assertNoScaffoldResidue(t, realDir)
}

// TestSvelteApplySymlinkedStandalone covers the same regression for
// apply --lang svelte . when the standalone project root is reached through a
// symlink.
func TestSvelteApplySymlinkedStandalone(t *testing.T) {
	skipUnlessShellFixture(t)

	work := t.TempDir()
	binDir := installFakeNpm(t, work)
	link, realDir := setupSymlinkedRoot(t, work)
	if err := os.WriteFile(filepath.Join(realDir, "scbake.toml"), nil, 0o600); err != nil {
		t.Fatalf("writing starting manifest: %v", err)
	}

	output, err := runScbake(t, link, binDir, "apply", "--lang", "svelte", ".")
	if err != nil {
		t.Fatalf("apply --lang svelte . in a symlinked project failed: %v\n%s", err, output)
	}

	// The package name follows the resolved directory basename, while the
	// manifest records the standalone project at path ".".
	assertScaffoldedSvelteProject(t, realDir, "real", ".")
	assertNoScaffoldResidue(t, realDir)
}

// TestSvelteApplyCollisionPreservesUserFile verifies the fail-closed collision
// preflight: an existing top-level destination is preserved and nothing else
// from the scaffold is moved.
func TestSvelteApplyCollisionPreservesUserFile(t *testing.T) {
	skipUnlessShellFixture(t)

	work := t.TempDir()
	binDir := installFakeNpm(t, work)
	target := filepath.Join(work, "coll")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("creating target: %v", err)
	}
	userFile := filepath.Join(target, "index.html")
	const userContent = "USER CONTENT\n"
	if err := os.WriteFile(userFile, []byte(userContent), 0o600); err != nil {
		t.Fatalf("writing user file: %v", err)
	}

	output, err := runScbake(t, work, binDir, "apply", "--lang", "svelte", "coll")
	if err == nil {
		t.Fatalf("collision unexpectedly succeeded:\n%s", output)
	}
	if !strings.Contains(output, "already exists") {
		t.Errorf("collision error does not mention the existing destination:\n%s", output)
	}
	got, readErr := os.ReadFile(userFile) //nolint:gosec // test-controlled path
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
		t.Errorf("collision moved additional scaffold entries: %v", entries)
	}
}

// TestSvelteNewDryRunWritesNothing verifies that the offline plan makes no
// filesystem changes.
func TestSvelteNewDryRunWritesNothing(t *testing.T) {
	skipUnlessShellFixture(t)

	work := t.TempDir()
	binDir := installFakeNpm(t, work)

	output, err := runScbake(t, work, binDir, "new", "sv", "--lang", "svelte", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(work, "sv")); !os.IsNotExist(err) {
		t.Errorf("dry-run created the project directory: %v", err)
	}
	assertNoScaffoldResidue(t, work)
}

func skipUnlessShellFixture(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the offline fake npm fixture is a POSIX shell script")
	}
}

func installFakeNpm(t *testing.T, work string) string {
	t.Helper()
	binDir := filepath.Join(work, "fake-bin")
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		t.Fatalf("creating fake npm dir: %v", err)
	}
	npmPath := filepath.Join(binDir, "npm")
	if err := os.WriteFile(npmPath, []byte(fakeCreateViteNpm), 0o700); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("writing fake npm: %v", err)
	}
	return binDir
}

// setupSymlinkedRoot creates work/real and a work/link symlink to it.
func setupSymlinkedRoot(t *testing.T, work string) (link, realDir string) {
	t.Helper()
	realDir = filepath.Join(work, "real")
	if err := os.Mkdir(realDir, 0o750); err != nil {
		t.Fatalf("creating symlink target: %v", err)
	}
	link = filepath.Join(work, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatalf("creating root symlink: %v", err)
	}
	return link, realDir
}

// setupSymlinkedRepo creates a repository fixture reached through a symlink:
// real/.git, real/scbake.toml, and an empty real/apps/web.
func setupSymlinkedRepo(t *testing.T, work string) (link, realDir string) {
	t.Helper()
	link, realDir = setupSymlinkedRoot(t, work)
	if err := os.MkdirAll(filepath.Join(realDir, "apps", "web"), 0o750); err != nil {
		t.Fatalf("creating monorepo target: %v", err)
	}
	// An empty marker is enough for root discovery to treat realDir/ as the
	// repository boundary; no Git command runs in this fixture.
	if err := os.Mkdir(filepath.Join(realDir, ".git"), 0o750); err != nil {
		t.Fatalf("creating git marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "scbake.toml"), nil, 0o600); err != nil {
		t.Fatalf("writing starting manifest: %v", err)
	}
	return link, realDir
}

func runScbake(t *testing.T, dir, binDir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binaryPath, args...) //nolint:gosec // test-controlled binary and arguments
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PWD="+dir,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_AUTHOR_NAME=Test Runner",
		"GIT_AUTHOR_EMAIL=runner@test.com",
		"GIT_COMMITTER_NAME=Test Runner",
		"GIT_COMMITTER_EMAIL=runner@test.com",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

// assertScaffoldedSvelteFiles checks the generated project files and package
// name without assuming where the manifest lives.
func assertScaffoldedSvelteFiles(t *testing.T, project, wantName string) {
	t.Helper()

	var pkg struct {
		Name string `json:"name"`
	}
	data, err := os.ReadFile(filepath.Join(project, "package.json")) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading package.json: %v", err)
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("parsing package.json: %v", err)
	}
	if pkg.Name != wantName {
		t.Errorf("package.json name = %q, want %q", pkg.Name, wantName)
	}

	for _, rel := range []string{"index.html", ".gitignore", "svelte.config.js", "src", "public", "package-lock.json", "node_modules"} {
		if _, err := os.Stat(filepath.Join(project, rel)); err != nil {
			t.Errorf("scaffolded project missing %s: %v", rel, err)
		}
	}
}

// assertScaffoldedSvelteProject checks the generated project and its manifest.
func assertScaffoldedSvelteProject(t *testing.T, project, wantName, wantPath string) {
	t.Helper()
	assertScaffoldedSvelteFiles(t, project, wantName)
	assertManifestProject(t, project, wantPath, "")
}

// assertManifestProject checks the svelte language and project path in a
// manifest. A non-empty wantName additionally checks the recorded project name.
func assertManifestProject(t *testing.T, project, wantPath, wantName string) {
	t.Helper()

	m := decodeManifest(t, project)
	var matches int
	for _, p := range m.Projects {
		if p.Path != wantPath {
			continue
		}
		matches++
		if p.Language != "svelte" {
			t.Errorf("manifest project %q language = %q, want svelte", wantPath, p.Language)
		}
		if wantName != "" && p.Name != wantName {
			t.Errorf("manifest project %q name = %q, want %q", wantPath, p.Name, wantName)
		}
	}
	if matches != 1 {
		t.Fatalf("manifest has %d projects at path %q, want 1", matches, wantPath)
	}
}

// assertNoScaffoldResidue fails when transaction state (.scbake), a staging
// directory, or another scaffold artifact survived a successful run.
func assertNoScaffoldResidue(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if name == ".scbake" || strings.HasPrefix(name, "stage-") || strings.HasPrefix(name, "scaffold-") {
			t.Errorf("scaffold residue left behind: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}
