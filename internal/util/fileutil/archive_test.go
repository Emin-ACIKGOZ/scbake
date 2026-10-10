// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package fileutil

import (
	"bytes"
	"os"
	"path/filepath"
	"scbake/internal/testutil"
	"strings"
	"testing"
)

func TestExtractTarGz_Basic(t *testing.T) {
	destDir := t.TempDir()
	archive := testutil.CreateTarGz(map[string]string{
		"templates/main.go.tpl": "package main",
		"templates/handler.tpl": "func handler() {}",
	})

	if err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20); err != nil {
		t.Fatalf("ExtractTarGz failed: %v", err)
	}

	content, err := SafeReadFile(destDir, "templates/main.go.tpl")
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != "package main" {
		t.Errorf("expected 'package main', got %q", string(content))
	}
}

func TestExtractTarGz_PathTraversalRejection(t *testing.T) {
	destDir := t.TempDir()
	archive := testutil.CreateTarGz(map[string]string{
		"../escape.txt": "secret data",
	})

	err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20)
	if err == nil {
		t.Error("expected ExtractTarGz to reject path traversal attempt")
	}
	if !strings.Contains(err.Error(), "path traversal rejected") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExtractTarGz_DecompressionBombProtection(t *testing.T) {
	destDir := t.TempDir()
	const maxLimit = 100

	oversizedPayload := bytes.Repeat([]byte("A"), 500)
	archive := testutil.CreateTarGz(map[string]string{
		"large.txt": string(oversizedPayload),
	})

	if err := ExtractTarGz(bytes.NewReader(archive), destDir, maxLimit); err != nil {
		t.Fatalf("ExtractTarGz failed unexpectedly: %v", err)
	}

	info, err := os.Stat(filepath.Join(destDir, "large.txt"))
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if info.Size() > maxLimit {
		t.Errorf("decompression limit failed: extracted size %d exceeds maxLimit %d", info.Size(), maxLimit)
	}
}
