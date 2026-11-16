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
