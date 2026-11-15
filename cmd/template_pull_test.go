// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"scbake/internal/templateregistry"
	"scbake/internal/testutil"
	"strings"
	"sync/atomic"
	"testing"
)

// registryCacheRoot returns the cache root the CLI resolves from the isolated
// XDG directories installed by isolateXDGDirs.
func registryCacheRoot(t *testing.T) string {
	t.Helper()
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("os.UserCacheDir failed: %v", err)
	}
	return filepath.Join(cacheDir, "scbake", "templates")
}

// TestTemplatePull_RejectsUnknownName is a regression test for #166: pulling a
// name that is neither a built-in template nor a language pack cannot produce an
// override, so the pull must be refused before any network request or cache
// write.
func TestTemplatePull_RejectsUnknownName(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)
	isolateXDGDirs(t)

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(testutil.CreateTarGz(map[string]string{"main.go.tpl": "package main"}))
	}))
	defer srv.Close()

	if err := executeCLI("template", "registry", "add", "local", srv.URL); err != nil {
		t.Fatalf("registry add failed: %v", err)
	}

	// registry add records registry precedence in the cache root, so clear it to
	// observe whether a rejected pull touches the cache at all.
	cacheRoot := registryCacheRoot(t)
	if err := os.RemoveAll(cacheRoot); err != nil {
		t.Fatalf("clearing cache root failed: %v", err)
	}

	err := executeCLI("template", "pull", "rustfmt", "--registry", "local")
	if err == nil {
		t.Fatal("pull of unknown template succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "rustfmt") {
		t.Errorf("error should name the rejected template, got %q", err)
	}
	if !strings.Contains(err.Error(), "built-in templates and language packs") {
		t.Errorf("error should explain the override-only scope, got %q", err)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("rejected pull made %d HTTP request(s), want 0", got)
	}
	if _, statErr := os.Stat(cacheRoot); !os.IsNotExist(statErr) {
		t.Errorf("rejected pull created cache directory %s (stat err: %v)", cacheRoot, statErr)
	}
}

// TestTemplatePull_AllowsLanguagePackName verifies that language packs remain
// valid pull targets, since a registry may override files of the Go language
// pack even though "go" is not a tooling template.
func TestTemplatePull_AllowsLanguagePackName(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)
	isolateXDGDirs(t)

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(testutil.CreateTarGz(map[string]string{"go.mod.tpl": "module example"}))
	}))
	defer srv.Close()

	if err := executeCLI("template", "registry", "add", "local", srv.URL); err != nil {
		t.Fatalf("registry add failed: %v", err)
	}

	out := captureStdout(t, func() {
		if pullErr := executeCLI("template", "pull", "go", "--registry", "local"); pullErr != nil {
			t.Errorf("pull of language pack failed: %v", pullErr)
		}
	})
	if !strings.Contains(out, `Pulled template "go"`) {
		t.Errorf("pull output = %q, want confirmation naming go", out)
	}
	if got := requests.Load(); got == 0 {
		t.Error("pull of language pack made no HTTP request, want at least one")
	}

	cachePath := filepath.Join(registryCacheRoot(t), "local", "go")
	if _, statErr := os.Stat(cachePath); statErr != nil {
		t.Errorf("pulled language pack missing from cache at %s: %v", cachePath, statErr)
	}
}

// TestPull_ReportsCacheHit is a regression test for issue #164: a pull served
// from a valid cache must say so instead of claiming the template was pulled
// again, and it must not make a request. It covers both the explicit
// --registry branch and the all-registries scan branch.
func TestPull_ReportsCacheHit(t *testing.T) { //nolint:cyclop // assertion-heavy regression test
	tests := []struct {
		name        string
		pullArgs    []string
		wantFetched string
		wantCached  string
	}{
		{
			name:        "explicit registry",
			pullArgs:    []string{"template", "pull", "go", "--registry", "acme"},
			wantFetched: `Pulled template "go" from registry "acme"` + "\n",
			wantCached:  `Template "go" from registry "acme" is up to date (cached)` + "\n",
		},
		{
			name:        "registry scan",
			pullArgs:    []string{"template", "pull", "go"},
			wantFetched: `Pulled template "go" from registry "acme"` + "\n",
			wantCached:  `Template "go" from registry "acme" is up to date (cached)` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateXDGDirs(t)
			resetFlags()
			t.Cleanup(resetFlags)

			var requests atomic.Int32
			var served atomic.Value
			served.Store("old body")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				content, _ := served.Load().(string)
				_, _ = w.Write(testutil.CreateTarGz(map[string]string{"main.go.tpl": content}))
			}))
			defer srv.Close()

			m, err := templateregistry.NewManager()
			if err != nil {
				t.Fatalf("NewManager failed: %v", err)
			}
			if err := m.Add("acme", srv.URL, "", "", ""); err != nil {
				t.Fatalf("Add failed: %v", err)
			}
			cachedFile := filepath.Join(templateregistry.TemplateCachePath(m.CacheDir(), "acme", "go"), "main.go.tpl")

			out := captureStdout(t, func() {
				if err := executeCLI(tt.pullArgs...); err != nil {
					t.Errorf("first pull failed: %v", err)
				}
			})
			if out != tt.wantFetched {
				t.Errorf("first pull output = %q, want %q", out, tt.wantFetched)
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("first pull made %d requests, want 1", got)
			}

			// Change what the server would serve. A cache hit must not fetch it.
			served.Store("new body")

			out = captureStdout(t, func() {
				if err := executeCLI(tt.pullArgs...); err != nil {
					t.Errorf("second pull failed: %v", err)
				}
			})
			if out != tt.wantCached {
				t.Errorf("second pull output = %q, want %q", out, tt.wantCached)
			}
			if strings.Contains(out, "Pulled") {
				t.Errorf("cache hit must not claim the template was pulled: %q", out)
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("cache hit made %d total requests, want 1", got)
			}

			//nolint:gosec // test temp directory
			content, err := os.ReadFile(cachedFile)
			if err != nil {
				t.Fatalf("reading cached template failed: %v", err)
			}
			if string(content) != "old body" {
				t.Errorf("cached content = %q, want the original body %q", content, "old body")
			}
		})
	}
}
