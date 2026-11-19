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
	for _, want := range []string{"monorepo root", "scbake update <project>", "from the manifest root", "--allow-ancestor-search", `"services/api"`, `"apps/web"`} {
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
