// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// writeMonorepoManifest seeds a Git-backed two-project monorepo manifest so
// apply and update from a subdirectory can discover the repository root.
func writeMonorepoManifest(t *testing.T, root string) {
	t.Helper()

	const manifest = `scbake_version = "v1.0.0"

[[projects]]
name = "api"
path = "services/api"
language = "go"

[[projects]]
name = "web"
path = "apps/web"
language = "svelte"
`
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte(manifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatalf("creating Git marker: %v", err)
	}
}

// contributingFirstLine returns the first line of a generated CONTRIBUTING.md
// with any CRLF terminator normalized away, so assertions are platform-neutral.
func contributingFirstLine(t *testing.T, path string) string {
	t.Helper()

	//nolint:gosec // test-controlled temp path
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	line := strings.SplitN(string(content), "\n", 2)[0]
	return strings.TrimRight(line, "\r\n")
}

// TestApplyAndUpdateCommunityContributingNamesOwningProject is the CLI-level
// regression test for issue #168: applying community to each monorepo
// subproject, and then updating each one, must name that subproject rather than
// the first manifest project.
func TestApplyAndUpdateCommunityContributingNamesOwningProject(t *testing.T) {
	resetFlags()
	updateConflictStrategyFlag = types.ConflictMerge

	root := t.TempDir()
	t.Chdir(root)
	writeMonorepoManifest(t, root)

	apiContributing := filepath.Join(root, "services", "api", "CONTRIBUTING.md")
	webContributing := filepath.Join(root, "apps", "web", "CONTRIBUTING.md")

	if err := executeCLI("apply", "--with", "community", "services/api"); err != nil {
		t.Fatalf("apply community to services/api failed: %v", err)
	}
	if err := executeCLI("apply", "--with", "community", "apps/web"); err != nil {
		t.Fatalf("apply community to apps/web failed: %v", err)
	}

	if got, want := contributingFirstLine(t, apiContributing), "# Contributing to api"; got != want {
		t.Errorf("api CONTRIBUTING heading after apply = %q, want %q", got, want)
	}
	if got, want := contributingFirstLine(t, webContributing), "# Contributing to web"; got != want {
		t.Errorf("web CONTRIBUTING heading after apply = %q, want %q", got, want)
	}

	// Deleting web's managed file forces update to render it again, so a stale
	// first-project fallback cannot hide behind the no-op path.
	if err := os.Remove(webContributing); err != nil {
		t.Fatalf("removing web CONTRIBUTING.md: %v", err)
	}

	if err := executeCLI("update", "services/api"); err != nil {
		t.Fatalf("update services/api failed: %v", err)
	}
	if err := executeCLI("update", "apps/web"); err != nil {
		t.Fatalf("update apps/web failed: %v", err)
	}

	if got, want := contributingFirstLine(t, apiContributing), "# Contributing to api"; got != want {
		t.Errorf("api CONTRIBUTING heading after update = %q, want %q", got, want)
	}
	if got, want := contributingFirstLine(t, webContributing), "# Contributing to web"; got != want {
		t.Errorf("web CONTRIBUTING heading after update = %q, want %q", got, want)
	}
}
