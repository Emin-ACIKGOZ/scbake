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

// fakeCreateViteNpm models the decisive create-vite 9.3.4 contract offline:
// a non-empty working directory cancels successfully and writes nothing, while
// an empty directory produces the Svelte template. `npm install` requires
// package.json, and `npm pkg set` is accepted. The script is intentionally a
// shell script: tests skip on Windows, where ./tests is not run in CI.
const fakeCreateViteNpm = `#!/bin/sh
set -eu

case "${1:-}" in
create)
	entries=$(ls -A)
	if [ -n "$entries" ]; then
		echo "Operation cancelled"
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
// issue #136. The scaffold target already contains scbake.toml and .scbake by
// the time the language handler runs; before the fix create-vite saw a
// non-empty directory, printed "Operation cancelled", exited 0, and the
// following npm install failed with ENOENT.
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

func runScbake(t *testing.T, dir, binDir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binaryPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
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

func assertScaffoldedSvelteProject(t *testing.T, project, wantName, wantPath string) {
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

	manifest, err := os.ReadFile(filepath.Join(project, "scbake.toml")) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading scbake.toml: %v", err)
	}
	text := string(manifest)
	if !strings.Contains(text, `language = "svelte"`) {
		t.Errorf("manifest does not record language svelte:\n%s", text)
	}
	if !strings.Contains(text, `path = "`+wantPath+`"`) {
		t.Errorf("manifest does not record path %s:\n%s", wantPath, text)
	}
}

// assertNoScaffoldResidue fails when the transaction journal, staging tree, or
// a top-level scaffold-* directory survived a successful run.
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
