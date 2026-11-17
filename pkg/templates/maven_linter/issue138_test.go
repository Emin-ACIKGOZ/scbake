// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package mavenlinter

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"scbake/internal/templateregistry"
	"scbake/internal/testutil"
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"testing"
)

// issue138CachedSnippet seeds a manifest-verified cache entry for the injected
// plugin snippet and returns the cache root.
func issue138CachedSnippet(t *testing.T, content string) string {
	t.Helper()

	archive := testutil.CreateTarGz(map[string]string{
		"pom_snippet.xml.tpl": content,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	m := templateregistry.NewManagerWithPaths(filepath.Join(dir, "registries.json"), cacheDir)
	if err := m.PullFromURL("acme", templateName, srv.URL, ""); err != nil {
		t.Fatalf("seeding registry cache: %v", err)
	}
	return cacheDir
}

// issue138Snippet returns the XML content of the InsertXMLTask in the plan.
func issue138Snippet(t *testing.T, taskList []types.Task) string {
	t.Helper()

	insertTask, ok := taskList[1].(*tasks.InsertXMLTask)
	if !ok {
		t.Fatalf("task 2 should be InsertXMLTask, got %T", taskList[1])
	}
	return insertTask.XMLContent
}

// TestIssue138_MavenConfiguredRegistryOverridesSnippet pins that a configured
// registry's cached snippet reaches the plan through GetTasksWithRegistries.
func TestIssue138_MavenConfiguredRegistryOverridesSnippet(t *testing.T) {
	const cached = "<plugin>issue138-configured-registry</plugin>"
	cacheDir := issue138CachedSnippet(t, cached)

	taskList, err := (&Handler{}).GetTasksWithRegistries(".", "", cacheDir, []string{"acme"})
	if err != nil {
		t.Fatalf("GetTasksWithRegistries: %v", err)
	}
	if got := issue138Snippet(t, taskList); got != cached {
		t.Errorf("snippet = %q, want the configured registry override %q", got, cached)
	}
}

// TestIssue138_MavenUnconfiguredRegistryFailsClosed pins that nil, empty, and
// non-matching allowlists ignore the cached snippet in both entry points.
func TestIssue138_MavenUnconfiguredRegistryFailsClosed(t *testing.T) {
	const cached = "<plugin>issue138-must-not-appear</plugin>"
	cacheDir := issue138CachedSnippet(t, cached)

	baseline, err := (&Handler{}).GetTasks(".", "", "")
	if err != nil {
		t.Fatalf("embedded baseline: %v", err)
	}
	embedded := issue138Snippet(t, baseline)

	cases := []struct {
		name  string
		tasks func() ([]types.Task, error)
	}{
		{
			name: "nil allowlist",
			tasks: func() ([]types.Task, error) {
				return (&Handler{}).GetTasksWithRegistries(".", "", cacheDir, nil)
			},
		},
		{
			name: "empty allowlist",
			tasks: func() ([]types.Task, error) {
				return (&Handler{}).GetTasksWithRegistries(".", "", cacheDir, []string{})
			},
		},
		{
			name: "unconfigured name",
			tasks: func() ([]types.Task, error) {
				return (&Handler{}).GetTasksWithRegistries(".", "", cacheDir, []string{"other"})
			},
		},
		{
			name: "plain GetTasks delegates fail-closed",
			tasks: func() ([]types.Task, error) {
				return (&Handler{}).GetTasks(".", "", cacheDir)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			taskList, err := tc.tasks()
			if err != nil {
				t.Fatalf("GetTasks: %v", err)
			}
			got := issue138Snippet(t, taskList)
			if got != embedded {
				t.Errorf("snippet came from an unconfigured registry cache: %q", got)
			}
		})
	}
}
