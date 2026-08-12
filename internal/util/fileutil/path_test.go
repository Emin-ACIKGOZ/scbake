// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafePath(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		baseDir    string
		targetPath string
		wantErr    bool
	}{
		{
			name:       "valid relative path",
			baseDir:    tmpDir,
			targetPath: "services/api/go.mod",
			wantErr:    false,
		},
		{
			name:       "valid same directory",
			baseDir:    tmpDir,
			targetPath: ".",
			wantErr:    false,
		},
		{
			name:       "valid nested relative path",
			baseDir:    tmpDir,
			targetPath: "./apps/web/package.json",
			wantErr:    false,
		},
		{
			name:       "path traversal parent directory",
			baseDir:    tmpDir,
			targetPath: "../secret.txt",
			wantErr:    true,
		},
		{
			name:       "path traversal multi level escape",
			baseDir:    tmpDir,
			targetPath: "../../etc/passwd",
			wantErr:    true,
		},
		{
			name:       "path traversal nested escape",
			baseDir:    tmpDir,
			targetPath: "sub/../../outside.txt",
			wantErr:    true,
		},
		{
			name:       "empty base directory",
			baseDir:    "",
			targetPath: "file.txt",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafePath(tt.baseDir, tt.targetPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if !filepath.IsAbs(got) {
					t.Errorf("SafePath() returned non-absolute path: %s", got)
				}
			}
		})
	}
}

func TestSafeReadWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	fileName := "test.txt"
	content := []byte("hello world")

	// Test SafeWriteFile
	if err := SafeWriteFile(tmpDir, fileName, content, PrivateFilePerms); err != nil {
		t.Fatalf("SafeWriteFile failed: %v", err)
	}

	// Test SafeReadFile
	got, err := SafeReadFile(tmpDir, fileName)
	if err != nil {
		t.Fatalf("SafeReadFile failed: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("SafeReadFile content mismatch: got %q, want %q", string(got), string(content))
	}

	// Test SafeOpenFile
	f, err := SafeOpenFile(tmpDir, fileName, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("SafeOpenFile failed: %v", err)
	}
	_ = f.Close()

	// Test Traversal Rejection on Write
	if err := SafeWriteFile(tmpDir, "../escape.txt", content, PrivateFilePerms); err == nil {
		t.Errorf("SafeWriteFile should reject traversal path")
	}

	// Test Traversal Rejection on Read
	if _, err := SafeReadFile(tmpDir, "../escape.txt"); err == nil {
		t.Errorf("SafeReadFile should reject traversal path")
	}
}
