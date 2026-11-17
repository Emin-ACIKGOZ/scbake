// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"scbake/internal/testutil"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// issue138Marker is the unique line the fake registry template emits. Seeing it
// in a generated project proves the registry cache supplied the file.
const issue138Marker = "# REGISTRY"

// issue138RegistryServer serves a small editorconfig template tarball over
// loopback. httptest picks a free port and the server is always closed.
func issue138RegistryServer(t *testing.T) *httptest.Server {
	t.Helper()

	archive := testutil.CreateTarGz(map[string]string{
		".editorconfig.tpl": "root = true\n" + issue138Marker + "\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			_, _ = w.Write(archive)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// issue138Setup isolates HOME/XDG dirs and chdirs into a fresh working
// directory so the generated project lands under the test temp dir.
func issue138Setup(t *testing.T) string {
	t.Helper()

	home := isolateUserDirs(t)
	work := filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatalf("creating work dir: %v", err)
	}
	t.Chdir(work)
	return work
}

// issue138MustRun runs the CLI and fails the test on a non-zero exit.
func issue138MustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runCLI(args...)
	if err != nil {
		t.Fatalf("scbake %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// issue138RegistryCacheDir is <user cache>/scbake/templates/<name>.
func issue138RegistryCacheDir(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(userCacheDir(t), "scbake", "templates", name)
}

// issue138AssertBuiltin fails unless the generated .editorconfig is the embedded
// template (second line "root = true", no registry marker).
func issue138AssertBuiltin(t *testing.T, projectPath string) {
	t.Helper()

	content, err := fileutil.SafeReadFile(projectPath, ".editorconfig")
	if err != nil {
		t.Fatalf("reading generated .editorconfig: %v", err)
	}
	if strings.Contains(string(content), issue138Marker) {
		t.Errorf("generated .editorconfig still comes from the registry cache:\n%s", string(content))
	}
	lines := strings.Split(string(content), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[1]) != "root = true" {
		t.Errorf("expected embedded .editorconfig (line 2 'root = true'), got:\n%s", string(content))
	}
}

func issue138AssertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, stat err: %v", path, err)
	}
}

// TestIssue138_RemovedRegistryDoesNotShadowBuiltin is the literal issue
// reproduction: pull a template, remove its registry, scaffold, and the
// built-in must win while the registry cache directory is gone.
func TestIssue138_RemovedRegistryDoesNotShadowBuiltin(t *testing.T) {
	work := issue138Setup(t)
	srv := issue138RegistryServer(t)

	issue138MustRun(t, "template", "registry", "add", "team", srv.URL)
	issue138MustRun(t, "template", "pull", "editorconfig", "--registry", "team")

	teamCache := issue138RegistryCacheDir(t, "team")
	if _, err := os.Stat(teamCache); err != nil {
		t.Fatalf("expected pulled template cache at %s: %v", teamCache, err)
	}

	issue138MustRun(t, "template", "registry", "remove", "team")
	issue138AssertMissing(t, teamCache)

	issue138MustRun(t, "new", "issue138-a", "--with", "editorconfig")
	issue138AssertBuiltin(t, filepath.Join(work, "issue138-a"))
}

// TestIssue138_RemovedRegistryShadowWhileAnotherConfigured covers the case that
// kills an "empty registry list disables the cache" shortcut: another registry
// stays configured, but the removed one's cache must not be reachable.
func TestIssue138_RemovedRegistryShadowWhileAnotherConfigured(t *testing.T) {
	work := issue138Setup(t)
	srv := issue138RegistryServer(t)

	issue138MustRun(t, "template", "registry", "add", "team", srv.URL)
	// A second registry that stays configured; its URL is never contacted.
	issue138MustRun(t, "template", "registry", "add", "other", "http://127.0.0.1:1")
	issue138MustRun(t, "template", "pull", "editorconfig", "--registry", "team")

	teamCache := issue138RegistryCacheDir(t, "team")
	otherCache := issue138RegistryCacheDir(t, "other")
	issue138MustRun(t, "template", "registry", "remove", "team")
	issue138AssertMissing(t, teamCache)
	if _, err := os.Stat(otherCache); !os.IsNotExist(err) {
		t.Errorf("removing team unexpectedly touched another registry's cache %s: %v", otherCache, err)
	}

	list := issue138MustRun(t, "template", "registry", "list")
	if !strings.Contains(list, "other") {
		t.Fatalf("expected registry 'other' to remain configured, list:\n%s", list)
	}

	issue138MustRun(t, "new", "issue138-b", "--with", "editorconfig")
	issue138AssertBuiltin(t, filepath.Join(work, "issue138-b"))
}

// TestIssue138_CacheWithoutConfigDoesNotShadowBuiltin is the structural case:
// after a pull, delete registries.json entirely (no `remove` command ran). The
// cache remains on disk but must be inert.
func TestIssue138_CacheWithoutConfigDoesNotShadowBuiltin(t *testing.T) {
	work := issue138Setup(t)
	srv := issue138RegistryServer(t)

	issue138MustRun(t, "template", "registry", "add", "team", srv.URL)
	issue138MustRun(t, "template", "pull", "editorconfig", "--registry", "team")

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("cannot determine user config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "scbake", "registries.json")
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("deleting %s: %v", configPath, err)
	}

	teamCache := issue138RegistryCacheDir(t, "team")
	if _, err := os.Stat(teamCache); err != nil {
		t.Fatalf("expected inert cache to remain at %s: %v", teamCache, err)
	}

	issue138MustRun(t, "new", "issue138-c", "--with", "editorconfig")
	issue138AssertBuiltin(t, filepath.Join(work, "issue138-c"))
}

// TestIssue138_UpdateAfterRemoveUsesBuiltin pins the same invariant on the
// update path, which resolves templates at execution time.
func TestIssue138_UpdateAfterRemoveUsesBuiltin(t *testing.T) {
	work := issue138Setup(t)
	projectPath := filepath.Join(work, "issue138-d")

	// Scaffold with the built-in before any registry exists.
	issue138MustRun(t, "new", "issue138-d", "--with", "editorconfig")
	issue138AssertBuiltin(t, projectPath)

	srv := issue138RegistryServer(t)
	issue138MustRun(t, "template", "registry", "add", "team", srv.URL)
	issue138MustRun(t, "template", "pull", "editorconfig", "--registry", "team")
	issue138MustRun(t, "template", "registry", "remove", "team")

	t.Chdir(projectPath)
	issue138MustRun(t, "update")
	issue138AssertBuiltin(t, projectPath)
}

// TestIssue138_DryRunNamesSource pins dry-run source disclosure before and
// after removal.
func TestIssue138_DryRunNamesSource(t *testing.T) {
	work := issue138Setup(t)
	srv := issue138RegistryServer(t)

	issue138MustRun(t, "template", "registry", "add", "team", srv.URL)
	issue138MustRun(t, "template", "pull", "editorconfig", "--registry", "team")

	before := issue138MustRun(t, "new", "issue138-e1", "--with", "editorconfig", "--dry-run")
	if !strings.Contains(before, "registry: team") {
		t.Errorf("dry-run with a configured registry should name 'registry: team', got:\n%s", before)
	}
	issue138AssertMissing(t, filepath.Join(work, "issue138-e1"))

	issue138MustRun(t, "template", "registry", "remove", "team")

	after := issue138MustRun(t, "new", "issue138-e2", "--with", "editorconfig", "--dry-run")
	if strings.Contains(after, "registry: team") {
		t.Errorf("dry-run after removal still names the removed registry, got:\n%s", after)
	}
	if !strings.Contains(after, "[built-in]") {
		t.Errorf("dry-run after removal should name '[built-in]', got:\n%s", after)
	}
	issue138AssertMissing(t, filepath.Join(work, "issue138-e2"))
}
