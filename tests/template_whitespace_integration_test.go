// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedProjectIsWhitespaceClean is the end-to-end regression guard for
// issue #275: a freshly generated Go project, including its committed
// .scbake-base copies, must end every file with exactly one newline, contain no
// trailing whitespace, and already be gofmt-clean.
func TestGeneratedProjectIsWhitespaceClean(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	out, err := runCLI("new", "demo", "--lang", "go",
		"--with", "makefile,editorconfig,ci_github,community,go_linter")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	project := filepath.Join(tmpDir, "demo")
	err = filepath.WalkDir(project, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".scbake" {
				return fs.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // generated test project
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(project, path)
		if err != nil {
			return err
		}
		assertGeneratedTextClean(t, filepath.ToSlash(rel), data)
		return nil
	})
	if err != nil {
		t.Fatalf("scanning generated project: %v", err)
	}

	gofmtPath, err := exec.LookPath("gofmt")
	if err != nil {
		t.Fatalf("gofmt is required for this regression test: %v", err)
	}
	gofmtOut, err := exec.Command(gofmtPath, "-l", project).CombinedOutput() //nolint:gosec // fixed tool and test temp path
	if err != nil {
		t.Fatalf("gofmt -l failed: %v\n%s", err, gofmtOut)
	}
	if len(bytes.TrimSpace(gofmtOut)) != 0 {
		t.Errorf("generated Go is not gofmt-clean:\n%s", gofmtOut)
	}
}

// assertGeneratedTextClean enforces the byte-level contract every generated
// text file must satisfy: a single trailing newline and no line that ends in a
// space or tab.
func assertGeneratedTextClean(t *testing.T, name string, data []byte) {
	t.Helper()

	if len(data) == 0 {
		t.Errorf("%s: generated file is empty", name)
		return
	}
	if data[len(data)-1] != '\n' {
		t.Errorf("%s: generated file does not end with a newline", name)
	}
	if bytes.HasSuffix(data, []byte("\n\n")) {
		t.Errorf("%s: generated file ends with more than one newline", name)
	}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			t.Errorf("%s:%d: trailing whitespace", name, i+1)
		}
	}
}
