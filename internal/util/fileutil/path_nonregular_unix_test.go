// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package fileutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// nonRegularTimeout bounds how long a fileutil call may take on a path that must
// be rejected without opening it. Opening a FIFO for reading or writing blocks
// until a peer appears, so a regression hangs here instead of failing.
const nonRegularTimeout = 2 * time.Second

type fileOpResult struct {
	data []byte
	err  error
}

// runFileOp runs op in a goroutine and fails the test if it does not return
// within nonRegularTimeout.
func runFileOp(t *testing.T, what string, op func() ([]byte, error)) ([]byte, error) {
	t.Helper()

	done := make(chan fileOpResult, 1)
	go func() {
		data, err := op()
		done <- fileOpResult{data: data, err: err}
	}()

	select {
	case got := <-done:
		return got.data, got.err
	case <-time.After(nonRegularTimeout):
		t.Fatalf("%s blocked for %s; a regression would hang the CLI", what, nonRegularTimeout)
		return nil, nil
	}
}

// TestFileHelpers_FIFORejected is a regression test for issue #218: a managed
// path replaced by a FIFO must never be opened. SafeReadFile, SafeOpenFile (read
// and write intent) and SafeWriteFile all guard against it, which also covers
// transaction backups of managed files and base shadows.
func TestFileHelpers_FIFORejected(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "managed.txt"), 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	ops := []struct {
		name string
		op   func() ([]byte, error)
	}{
		{
			name: "SafeReadFile",
			op: func() ([]byte, error) {
				return SafeReadFile(dir, "managed.txt")
			},
		},
		{
			name: "SafeOpenFile read",
			op: func() ([]byte, error) {
				f, err := SafeOpenFile(dir, "managed.txt", os.O_RDONLY, 0)
				if f != nil {
					_ = f.Close()
				}
				return nil, err
			},
		},
		{
			name: "SafeOpenFile write",
			op: func() ([]byte, error) {
				f, err := SafeOpenFile(dir, "managed.txt", os.O_WRONLY|os.O_CREATE, FilePerms)
				if f != nil {
					_ = f.Close()
				}
				return nil, err
			},
		},
		{
			name: "SafeWriteFile",
			op: func() ([]byte, error) {
				return nil, SafeWriteFile(dir, "managed.txt", []byte("x"), FilePerms)
			},
		},
	}

	for _, tt := range ops {
		t.Run(tt.name, func(t *testing.T) {
			data, err := runFileOp(t, tt.name, tt.op)
			if err == nil {
				t.Fatalf("%s accepted a FIFO without error (data=%q)", tt.name, data)
			}
			if !strings.Contains(err.Error(), "not a regular file") {
				t.Errorf("%s error = %v, want it to mention %q", tt.name, err, "not a regular file")
			}
		})
	}
}

// TestSafeReadFile_DirectoryRejected verifies a directory is rejected with the
// same clear diagnostic instead of an open/read failure.
func TestSafeReadFile_DirectoryRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), DirPerms); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := runFileOp(t, "SafeReadFile", func() ([]byte, error) {
		return SafeReadFile(dir, "subdir")
	})
	if err == nil {
		t.Fatal("SafeReadFile read a directory without error")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("SafeReadFile error = %v, want it to mention %q", err, "not a regular file")
	}
}

// TestSafeReadFile_MissingReturnsNotExist verifies the guard preserves the
// not-exist contract that callers rely on.
func TestSafeReadFile_MissingReturnsNotExist(t *testing.T) {
	dir := t.TempDir()

	_, err := runFileOp(t, "SafeReadFile", func() ([]byte, error) {
		return SafeReadFile(dir, "missing.txt")
	})
	if err == nil {
		t.Fatal("SafeReadFile read a missing file without error")
	}
	if !errors.Is(err, fs.ErrNotExist) || !os.IsNotExist(err) {
		t.Errorf("SafeReadFile error = %v, want fs.ErrNotExist", err)
	}
}

// TestFileHelpers_MissingPathAllowed verifies missing paths are still openable
// for creation (the guard must not reject a not-yet-existing target).
func TestFileHelpers_MissingPathAllowed(t *testing.T) {
	dir := t.TempDir()

	_, err := runFileOp(t, "SafeOpenFile create", func() ([]byte, error) {
		f, err := SafeOpenFile(dir, "created.txt", os.O_WRONLY|os.O_CREATE, FilePerms)
		if f != nil {
			_ = f.Close()
		}
		return nil, err
	})
	if err != nil {
		t.Fatalf("SafeOpenFile could not create a missing file: %v", err)
	}

	_, err = runFileOp(t, "SafeWriteFile create", func() ([]byte, error) {
		return nil, SafeWriteFile(dir, "written.txt", []byte("hello"), FilePerms)
	})
	if err != nil {
		t.Fatalf("SafeWriteFile could not create a missing file: %v", err)
	}

	data, err := runFileOp(t, "SafeReadFile", func() ([]byte, error) {
		return SafeReadFile(dir, "written.txt")
	})
	if err != nil {
		t.Fatalf("SafeReadFile could not read the created file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("SafeReadFile content = %q, want %q", data, "hello")
	}
}

// TestSafeReadFile_RegularFileAfterNonRegular verifies the guard does not affect
// ordinary reads: replacing the non-regular path with a regular file works.
func TestSafeReadFile_RegularFileAfterNonRegular(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed.txt")

	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	if _, err := runFileOp(t, "SafeReadFile on FIFO", func() ([]byte, error) {
		return SafeReadFile(dir, "managed.txt")
	}); err == nil {
		t.Fatal("SafeReadFile accepted a FIFO")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove FIFO: %v", err)
	}
	const content = "regular file content\n"
	if err := os.WriteFile(path, []byte(content), FilePerms); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	got, err := runFileOp(t, "SafeReadFile on regular file", func() ([]byte, error) {
		return SafeReadFile(dir, "managed.txt")
	})
	if err != nil {
		t.Fatalf("SafeReadFile on a regular file failed: %v", err)
	}
	if string(got) != content {
		t.Errorf("SafeReadFile content = %q, want %q", got, content)
	}
}
