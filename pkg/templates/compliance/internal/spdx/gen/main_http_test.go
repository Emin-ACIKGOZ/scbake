// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"scbake/pkg/templates/compliance/internal/spdx/version"
	"strings"
	"testing"
)

// newHTTPLoader points a loader at an httptest server so no test reaches the
// public network. The returned URL is the origin used to assert error text.
func newHTTPLoader(t *testing.T, handler http.HandlerFunc) (*loader, string) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &loader{
		baseURL: server.URL + "/",
		client:  server.Client(),
	}, server.URL
}

// newBodyLoader serves the same body for every request.
func newBodyLoader(t *testing.T, body string) (*loader, string) {
	t.Helper()

	return newHTTPLoader(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
}

func assertErrorContains(t *testing.T, err error, wants ...string) {
	t.Helper()

	if err == nil {
		t.Fatalf("got nil error; want an error containing %q", wants)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

// jsonBodyOfSize builds a valid JSON document with an ignored padding string so
// response-size boundary tests can produce exact byte counts.
func jsonBodyOfSize(t *testing.T, size int, prefix, suffix string) string {
	t.Helper()

	padding := size - len(prefix) - len(suffix)
	if padding < 0 {
		t.Fatalf("size %d is smaller than the JSON wrapper (%d bytes)", size, len(prefix)+len(suffix))
	}
	body := prefix + strings.Repeat("x", padding) + suffix
	if len(body) != size {
		t.Fatalf("built body is %d bytes, want %d", len(body), size)
	}
	return body
}

func seedOutputPair(t *testing.T) (archivePath, indexPath string, archiveSentinel, indexSentinel []byte) {
	t.Helper()

	dir := t.TempDir()
	archivePath = filepath.Join(dir, "licenses.zip")
	indexPath = filepath.Join(dir, "index.go")
	archiveSentinel = []byte("existing archive\n")
	indexSentinel = []byte("existing index\n")
	if err := os.WriteFile(archivePath, archiveSentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, indexSentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	return archivePath, indexPath, archiveSentinel, indexSentinel
}

func TestNewLoaderAcceptsValidTag(t *testing.T) {
	l, err := newLoader("", version.Tag)
	if err != nil {
		t.Fatalf("newLoader(%q) failed: %v", version.Tag, err)
	}
	if l.sourceDir != "" {
		t.Errorf("loader sourceDir = %q, want empty", l.sourceDir)
	}
	wantBaseURL := licenseRepoURL + version.Tag + "/"
	if l.baseURL != wantBaseURL {
		t.Errorf("loader baseURL = %q, want %q", l.baseURL, wantBaseURL)
	}
	if l.client == nil {
		t.Fatal("loader client is nil")
	}
	if l.client.Timeout != httpTimeout {
		t.Errorf("loader client timeout = %v, want %v", l.client.Timeout, httpTimeout)
	}
}

func TestNewLoaderRejectsInvalidTags(t *testing.T) {
	tests := []struct {
		name string
		tag  string
	}{
		{"empty", ""},
		{"no v prefix", "3.29.0"},
		{"slash", "v3.29.0/"},
		{"backslash", `v3.29.0\`},
		{"query", "v3.29.0?download=1"},
		{"fragment", "v3.29.0#details"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, err := newLoader("", tc.tag)
			if err == nil {
				t.Fatalf("newLoader(%q) succeeded; want an invalid-tag error", tc.tag)
			}
			if l != nil {
				t.Errorf("newLoader(%q) returned a loader alongside an error: %+v", tc.tag, l)
			}
			assertErrorContains(t, err, "invalid SPDX tag", fmt.Sprintf("%q", tc.tag))
		})
	}
}

func TestNewLoaderLocalSourceBypassesTagValidation(t *testing.T) {
	source := t.TempDir()
	for i, tag := range []string{"", "not-a-tag", "v3.29.0/extra", `v3.29.0\extra`, "v3.29.0?x", "v3.29.0#x"} {
		t.Run(fmt.Sprintf("case %d", i), func(t *testing.T) {
			l, err := newLoader(source, tag)
			if err != nil {
				t.Fatalf("newLoader(%q, %q) failed: %v", source, tag, err)
			}
			if l.sourceDir != source {
				t.Errorf("loader sourceDir = %q, want %q", l.sourceDir, source)
			}
			if l.baseURL != "" {
				t.Errorf("loader baseURL = %q, want empty for a local source", l.baseURL)
			}
			if l.client != nil {
				t.Errorf("loader client = %+v, want nil for a local source", l.client)
			}
		})
	}
}

func TestLoadListHTTPError(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			l, serverURL := newHTTPLoader(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			})

			_, err := l.loadList(context.Background())
			assertErrorContains(t, err,
				serverURL+"/"+licenseListPath,
				fmt.Sprintf("%d %s", status, http.StatusText(status)),
			)
		})
	}
}

func TestLoadTextHTTPError(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			l, serverURL := newHTTPLoader(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			})

			_, err := l.loadText(context.Background(), "MIT")
			assertErrorContains(t, err,
				serverURL+"/"+licenseDir+"/MIT.json",
				fmt.Sprintf("%d %s", status, http.StatusText(status)),
			)
		})
	}
}

func TestLoadListResponseSizeLimit(t *testing.T) {
	const (
		prefix = `{"licenseListVersion":"test-1.0","licenses":[],"pad":"`
		suffix = `"}`
	)

	// The cap is inclusive: a body of exactly maxResponseBytes is rejected. The
	// success case stops one byte below that boundary, and the oversized cases
	// prove the read is capped rather than merely comparing the full body length.
	for _, size := range []int{maxResponseBytes, maxResponseBytes + 1, 2 * maxResponseBytes} {
		t.Run(fmt.Sprintf("%d bytes fails", size), func(t *testing.T) {
			l, serverURL := newBodyLoader(t, jsonBodyOfSize(t, size, prefix, suffix))

			_, err := l.loadList(context.Background())
			assertErrorContains(t, err,
				serverURL+"/"+licenseListPath,
				fmt.Sprintf("exceeds %d bytes", maxResponseBytes),
			)
		})
	}

	t.Run("maxResponseBytes-1 succeeds", func(t *testing.T) {
		l, _ := newBodyLoader(t, jsonBodyOfSize(t, maxResponseBytes-1, prefix, suffix))

		list, err := l.loadList(context.Background())
		if err != nil {
			t.Fatalf("loadList with a %d-byte body failed: %v", maxResponseBytes-1, err)
		}
		if list.Version != "test-1.0" {
			t.Errorf("list version = %q, want test-1.0", list.Version)
		}
		if len(list.Licenses) != 0 {
			t.Errorf("list has %d licenses, want 0", len(list.Licenses))
		}
	})
}

func TestLoadTextResponseSizeLimit(t *testing.T) {
	const (
		prefix = `{"licenseId":"MIT","licenseText":"`
		suffix = `"}`
	)

	// The cap is inclusive: a body of exactly maxResponseBytes is rejected. The
	// success case stops one byte below that boundary, and the oversized cases
	// prove the read is capped rather than merely comparing the full body length.
	for _, size := range []int{maxResponseBytes, maxResponseBytes + 1, 2 * maxResponseBytes} {
		t.Run(fmt.Sprintf("%d bytes fails", size), func(t *testing.T) {
			l, serverURL := newBodyLoader(t, jsonBodyOfSize(t, size, prefix, suffix))

			_, err := l.loadText(context.Background(), "MIT")
			assertErrorContains(t, err,
				serverURL+"/"+licenseDir+"/MIT.json",
				fmt.Sprintf("exceeds %d bytes", maxResponseBytes),
			)
		})
	}

	t.Run("maxResponseBytes-1 succeeds", func(t *testing.T) {
		l, _ := newBodyLoader(t, jsonBodyOfSize(t, maxResponseBytes-1, prefix, suffix))

		text, err := l.loadText(context.Background(), "MIT")
		if err != nil {
			t.Fatalf("loadText with a %d-byte body failed: %v", maxResponseBytes-1, err)
		}
		wantLen := maxResponseBytes - 1 - len(prefix) - len(suffix)
		if len(text) != wantLen {
			t.Errorf("license text length = %d, want %d", len(text), wantLen)
		}
	})
}

func TestLoadMalformedJSON(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		l, _ := newBodyLoader(t, "{")

		_, err := l.loadList(context.Background())
		assertErrorContains(t, err, "parse "+licenseListPath)
	})

	t.Run("detail", func(t *testing.T) {
		l, _ := newBodyLoader(t, "{")

		_, err := l.loadText(context.Background(), "MIT")
		assertErrorContains(t, err, `parse license "MIT"`)
	})
}

func TestLoadTextRejectsMissingOrBlankText(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"missing", `{"licenseId":"MIT"}`},
		{"blank", "{\"licenseId\":\"MIT\",\"licenseText\":\" \\n\\t\"}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, _ := newBodyLoader(t, tc.body)

			_, err := l.loadText(context.Background(), "MIT")
			assertErrorContains(t, err, `license "MIT" has no licenseText`)
		})
	}
}

func TestLoadTextValidatesLicenseID(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantText string
		wantErr  string
	}{
		{
			name:    "mismatched id fails",
			body:    `{"licenseId":"Apache-2.0","licenseText":"MIT License\n"}`,
			wantErr: `license "MIT" returned details for "Apache-2.0"`,
		},
		{
			name:     "empty id is allowed",
			body:     `{"licenseId":"","licenseText":"MIT License\n"}`,
			wantText: "MIT License\n",
		},
		{
			name:     "matching id succeeds",
			body:     `{"licenseId":"MIT","licenseText":"MIT License\n"}`,
			wantText: "MIT License\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, _ := newBodyLoader(t, tc.body)

			text, err := l.loadText(context.Background(), "MIT")
			if tc.wantErr != "" {
				assertErrorContains(t, err, tc.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("loadText failed: %v", err)
			}
			if text != tc.wantText {
				t.Errorf("license text = %q, want %q", text, tc.wantText)
			}
		})
	}
}

// TestRunLeavesOutputsUntouchedOnSourceErrors covers the remaining source and
// list validation failures. run must fail before either output is written.
func TestRunLeavesOutputsUntouchedOnSourceErrors(t *testing.T) {
	tests := []struct {
		name   string
		source func(t *testing.T) string
		want   []string
	}{
		{
			name: "missing detail file",
			source: func(t *testing.T) string {
				t.Helper()
				source := writeLicenseSource(t, map[string]string{
					"Apache-2.0": "Apache License\n",
					"MIT":        "MIT License\n",
				})
				if err := os.Remove(filepath.Join(source, "json", "details", "MIT.json")); err != nil {
					t.Fatalf("remove fixture detail file: %v", err)
				}
				return source
			},
			want: []string{"load license texts", "MIT.json"},
		},
		{
			name: "missing licenseListVersion",
			source: func(t *testing.T) string {
				t.Helper()
				return writeRawLicenseList(t, `{"licenses":[{"licenseId":"MIT"}]}`)
			},
			want: []string{"missing licenseListVersion"},
		},
		{
			name: "empty licenseId",
			source: func(t *testing.T) string {
				t.Helper()
				return writeRawLicenseList(t, fmt.Sprintf(
					`{"licenseListVersion":%q,"licenses":[{"licenseId":""}]}`,
					version.Version,
				))
			},
			want: []string{"contains an empty licenseId"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.source(t)
			archivePath, indexPath, archiveSentinel, indexSentinel := seedOutputPair(t)

			err := run(context.Background(), config{
				sourceDir: source,
				outPath:   archivePath,
				indexPath: indexPath,
			})
			assertErrorContains(t, err, tc.want...)

			assertFileContents(t, archivePath, archiveSentinel)
			assertFileContents(t, indexPath, indexSentinel)
		})
	}
}
