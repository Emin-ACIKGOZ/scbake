// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestListProjectsRequiresManifest distinguishes an absent manifest from a
// present manifest with no projects, matching drift's missing-manifest error.
func TestListProjectsRequiresManifest(t *testing.T) {
	t.Run("absent manifest fails", func(t *testing.T) {
		work := t.TempDir()
		skipIfAncestorHasMarker(t, work)
		t.Chdir(work)

		out, err := runCLI("list", "projects")
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() == 0 {
			t.Fatalf("list projects with no manifest should exit non-zero, got err=%v\n%s", err, out)
		}
		if !strings.Contains(out, "no scbake.toml found") {
			t.Errorf("error should name the missing manifest, output:\n%s", out)
		}
	})

	t.Run("present empty manifest succeeds", func(t *testing.T) {
		work := t.TempDir()
		t.Chdir(work)
		body := "scbake_version = \"v0.3.0\"\nprojects = []\ntemplates = []\n"
		if err := os.WriteFile(filepath.Join(work, "scbake.toml"), []byte(body), 0o600); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		out, err := runCLI("list", "projects")
		if err != nil {
			t.Fatalf("list projects on an empty manifest failed: %v\n%s", err, out)
		}
		if !strings.Contains(out, "No projects found.") {
			t.Errorf("output should report an empty present manifest, got:\n%s", out)
		}
	})
}

// TestRootUpdateNamesMonorepoProjects pins the actionable dead end: a root
// manifest with no root templates must name the recorded projects and the
// invocation that updates one.
func TestRootUpdateNamesMonorepoProjects(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	body := `scbake_version = "v0.3.0"

[[projects]]
  name = "api"
  path = "services/api"
  language = "go"

[[projects]]
  name = "web"
  path = "apps/web"
  language = "svelte"

[[projects]]
  name = "root"
  path = "."

[[projects]]
  name = "dot-slash"
  path = "./"
`
	if err := os.WriteFile(filepath.Join(work, "scbake.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	out, err := runCLI("update")
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() == 0 {
		t.Fatalf("root update should exit non-zero, got err=%v\n%s", err, out)
	}
	for _, want := range []string{"monorepo root", "scbake update -- <project>", "from the manifest root", "--allow-ancestor-search", `'services/api'`, `'apps/web'`} {
		if !strings.Contains(out, want) {
			t.Errorf("root update error does not contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "for one of: .") || strings.Contains(out, `"./"`) {
		t.Errorf("root update suggested a root-equivalent project as a target:\n%s", out)
	}
}

// TestSubdirUpdateDoesNotClaimMonorepoRoot keeps the hint accurate when the
// target is a subdirectory of a markerless monorepo: it is not the root.
func TestSubdirUpdateDoesNotClaimMonorepoRoot(t *testing.T) {
	mono := setupMarkerlessMonorepo(t, "[[projects]]\nname = \"api\"\npath = \"services/api\"\nlanguage = \"go\"\n")
	sub := makeSubdir(t, mono, "docs")
	t.Chdir(sub)

	out, err := runCLI("update", "--allow-ancestor-search")
	if err == nil {
		t.Fatalf("subdirectory update should fail with no templates:\n%s", out)
	}
	if strings.Contains(out, "monorepo root") {
		t.Errorf("subdirectory update misidentified the target as the monorepo root:\n%s", out)
	}
	if !strings.Contains(out, "nothing to update") {
		t.Errorf("subdirectory update did not give the ordinary no-templates error:\n%s", out)
	}
}

// TestRootUpdateSanitizesManifestPaths pins the escaping of control characters
// in the monorepo hint. The manifest path is untrusted and is printed before
// validateManifestFields runs, so the command must render it safely.
func TestRootUpdateSanitizesManifestPaths(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	body := `scbake_version = "v0.3.0"

[[projects]]
  name = "api"
  path = "services/\u001b[31mapi"
  language = "go"
`
	if err := os.WriteFile(filepath.Join(work, "scbake.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	out, err := runCLI("update")
	if err == nil {
		t.Fatalf("root update should exit non-zero:\n%s", out)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("output contains a raw ESC byte:\n%q", out)
	}
	if !strings.Contains(out, `services/\x1b[31mapi`) {
		t.Errorf("output does not contain the escaped project path:\n%s", out)
	}
}

// TestRootUpdateDoesNotSuggestShellMetacharacterPaths ensures a malicious or
// corrupt manifest cannot place command substitution into the suggested
// command. Invalid paths are reported as ignored, single-quoted.
func TestRootUpdateDoesNotSuggestShellMetacharacterPaths(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	body := `scbake_version = "v0.3.0"

[[projects]]
  name = "evil"
  path = "$(touch PWNED)"
  language = "go"

[[projects]]
  name = "api"
  path = "services/api"
  language = "go"
`
	if err := os.WriteFile(filepath.Join(work, "scbake.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	out, err := runCLI("update")
	if err == nil {
		t.Fatalf("root update should exit non-zero:\n%s", out)
	}
	if strings.Contains(out, `"$(touch PWNED)"`) {
		t.Errorf("invalid path was suggested in a double-quoted command:\n%s", out)
	}
	if !strings.Contains(out, `'$(touch PWNED)'`) {
		t.Errorf("invalid path was not reported as an ignored single-quoted value:\n%s", out)
	}
	if !strings.Contains(out, `'services/api'`) {
		t.Errorf("valid project was not suggested:\n%s", out)
	}
}

// TestRootUpdateIgnoresNonCleanRecordedPaths keeps legacy non-clean relative
// entries inert: they are reported as ambiguous, not suggested as cleaned
// targets that the update selection would never match.
func TestRootUpdateIgnoresNonCleanRecordedPaths(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	body := `scbake_version = "v0.3.0"

[[projects]]
  name = "api"
  path = "./services/api"
  language = "go"

[[templates]]
  name = "makefile"
  path = "./services/api"
`
	if err := os.WriteFile(filepath.Join(work, "scbake.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(work, "services", "api"), 0o750); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	out, err := runCLI("update")
	if err == nil {
		t.Fatalf("root update should report the hint:\n%s", out)
	}
	if !strings.Contains(out, "Ambiguous or invalid recorded paths are ignored") {
		t.Errorf("hint did not mark the non-clean path ambiguous:\n%s", out)
	}
	if !strings.Contains(out, `'./services/api'`) {
		t.Errorf("hint did not show the recorded path as ignored:\n%s", out)
	}
	if strings.Contains(out, "for one of: 'services/api'") {
		t.Errorf("hint suggested a cleaned path the selection cannot match:\n%s", out)
	}
}

// TestRootUpdateHintSeparatesFlagLikePaths ensures a recorded path beginning
// with a dash is not presented as an option to update.
func TestRootUpdateHintSeparatesFlagLikePaths(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	body := `scbake_version = "v0.3.0"

[[projects]]
  name = "dash"
  path = "-x"
  language = "go"
`
	if err := os.WriteFile(filepath.Join(work, "scbake.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	out, err := runCLI("update")
	if err == nil {
		t.Fatalf("root update should report the hint:\n%s", out)
	}
	if !strings.Contains(out, "scbake update -- <project>") {
		t.Errorf("hint does not separate flags from the path:\n%s", out)
	}
	if !strings.Contains(out, "'-x'") {
		t.Errorf("hint did not show the dash-prefixed path:\n%s", out)
	}
}

// TestRootUpdateSuggestsRunnableMarkerlessCommand executes the documented form
// in a marker-less monorepo: ancestor search before the -- separator.
func TestRootUpdateSuggestsRunnableMarkerlessCommand(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	body := `scbake_version = "v0.3.0"

[[projects]]
  name = "api"
  path = "services/api"
  language = "go"

[[templates]]
  name = "makefile"
  path = "services/api"
`
	if err := os.WriteFile(filepath.Join(work, "scbake.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(work, "services", "api"), 0o750); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	out, err := runCLI("update", "--allow-ancestor-search", "--dry-run", "--", "services/api")
	if err != nil {
		t.Fatalf("documented marker-less invocation failed: %v\n%s", err, out)
	}
}
