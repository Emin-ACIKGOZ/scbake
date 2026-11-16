// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package spdx

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const embeddedArchiveSHA256 = "27946c40bd9d43cb8ff3b33ac17334c3765b75c442611f04c78a836e85d1236e"

func TestTextMIT(t *testing.T) {
	text, err := Text("MIT")
	if err != nil {
		t.Fatalf("Text(MIT) failed: %v", err)
	}
	if !strings.Contains(text, "MIT License") {
		t.Errorf("MIT text missing title:\n%s", text)
	}
	if !strings.Contains(text, "<year>") || !strings.Contains(text, "<copyright holders>") {
		t.Errorf("MIT text should retain SPDX placeholders:\n%s", text)
	}
}

func TestTextCaseInsensitive(t *testing.T) {
	upper, err := Text("MIT")
	if err != nil {
		t.Fatalf("Text(MIT) failed: %v", err)
	}

	lower, err := Text("mit")
	if err != nil {
		t.Fatalf("Text(mit) failed: %v", err)
	}

	if upper != lower {
		t.Error("license lookup should be case-insensitive")
	}
}

func TestTextUnknown(t *testing.T) {
	_, err := Text("NOT-A-REAL-LICENSE")
	if err == nil {
		t.Fatal("expected an error for an unknown SPDX license")
	}
	if !strings.Contains(err.Error(), "unknown SPDX license ID") {
		t.Errorf("error should name the unknown license, got: %v", err)
	}
}

func TestTextDeprecated(t *testing.T) {
	text, err := Text("GPL-2.0")
	if err != nil {
		t.Fatalf("Text(GPL-2.0) failed: %v", err)
	}
	if !strings.Contains(text, "GNU GENERAL PUBLIC LICENSE") {
		t.Errorf("deprecated GPL-2.0 text is missing or incomplete")
	}
}

func TestEveryEmbeddedLicenseResolves(t *testing.T) {
	for lower, canonical := range canonicalIDs {
		text, err := Text(lower)
		if err != nil {
			t.Fatalf("Text(%q) failed: %v", canonical, err)
		}
		if strings.TrimSpace(text) == "" {
			t.Fatalf("Text(%q) returned empty text", canonical)
		}
	}
}

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}

// TestEmbeddedArchiveIntegrity hashes the committed licenses.zip bytes. It
// never regenerates the archive, so the assertion is independent of the local
// Go toolchain's deflate output.
func TestEmbeddedArchiveIntegrity(t *testing.T) {
	sum := sha256.Sum256(licenseArchive)
	if got := hex.EncodeToString(sum[:]); got != embeddedArchiveSHA256 {
		t.Fatalf("embedded licenses.zip sha256 = %s, want %s; update the archive, index, and pinned hash together",
			got, embeddedArchiveSHA256)
	}

	zr, err := zip.NewReader(bytes.NewReader(licenseArchive), int64(len(licenseArchive)))
	if err != nil {
		t.Fatalf("open embedded licenses.zip: %v", err)
	}
	if len(zr.File) != len(canonicalIDs) {
		t.Fatalf("embedded archive has %d entries, canonicalIDs has %d", len(zr.File), len(canonicalIDs))
	}

	assertCanonicalCoverage(t, scanArchiveIDs(t, zr))
}

func archiveID(name string) (string, bool) {
	const prefix = "licenses/"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".txt") {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".txt")
	if id == "" || strings.ContainsAny(id, `/\`) || name != prefix+id+".txt" {
		return "", false
	}
	return id, true
}

func scanArchiveIDs(t *testing.T, zr *zip.Reader) map[string]string {
	t.Helper()

	seen := make(map[string]string, len(zr.File))
	for _, file := range zr.File {
		id, ok := archiveID(file.Name)
		if !ok {
			t.Errorf("embedded archive entry %q is not licenses/<ID>.txt", file.Name)
			continue
		}
		lower := strings.ToLower(id)
		if previous, ok := seen[lower]; ok {
			t.Errorf("embedded archive IDs %q and %q collide case-insensitively", previous, id)
			continue
		}
		seen[lower] = id
		if canonical, ok := canonicalIDs[lower]; !ok || canonical != id {
			t.Errorf("embedded archive entry %q does not match canonicalIDs[%q] = %q", file.Name, lower, canonical)
		}
	}
	return seen
}

func assertCanonicalCoverage(t *testing.T, seen map[string]string) {
	t.Helper()

	for lower, canonical := range canonicalIDs {
		if seen[lower] != canonical {
			t.Errorf("canonical ID %q has no matching embedded archive entry", canonical)
		}
	}
}
