// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"
	"scbake/internal/util/fileutil"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const lockTestWorkers = 10

// setupLockTestProject creates a scbake project with ten pre-created parallel
// target directories, matching the issue's reproduction.
func setupLockTestProject(t *testing.T) string {
	t.Helper()

	work := t.TempDir()
	t.Chdir(work)
	if _, err := runCLI("new", "conc", "--with", "git,editorconfig"); err != nil {
		t.Fatalf("creating test project: %v", err)
	}
	project := filepath.Join(work, "conc")
	for i := 1; i <= lockTestWorkers; i++ {
		if err := os.MkdirAll(filepath.Join(project, "par", fmt.Sprintf("p%d", i)), fileutil.DirPerms); err != nil {
			t.Fatal(err)
		}
	}
	// Run applies from the project root with relative targets so the manifest
	// records portable entries, matching the issue's reproduction.
	t.Chdir(project)
	return project
}

func applyTarget(index int) []string {
	return []string{"apply", "--with", "editorconfig", filepath.Join("par", fmt.Sprintf("p%d", index))}
}

func exitCodeOf(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	return exitErr.ExitCode(), true
}

// TestApplyBusyExits75 verifies a contended root fails fast with EX_TEMPFAIL,
// a clear message, and no mutation.
func TestApplyBusyExits75(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, fileutil.ManifestFileName)
	if err := os.WriteFile(manifestPath, nil, fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	lock, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("AcquireRootLock failed: %v", err)
	}
	defer func() { _ = lock.Release() }()

	output, err := runCLI("apply", "--with", "editorconfig", root)
	if err == nil {
		t.Fatal("apply succeeded while the root lock was held")
	}
	code, ok := exitCodeOf(err)
	if !ok {
		t.Fatalf("apply error is not an exit error: %v", err)
	}
	if code != fileutil.ExitBusy {
		t.Errorf("busy apply exit code = %d, want %d (output=%s)", code, fileutil.ExitBusy, output)
	}
	if !strings.Contains(output, "another scbake process is operating on this project") {
		t.Errorf("busy apply output does not explain the contention: %s", output)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".editorconfig")); !os.IsNotExist(statErr) {
		t.Errorf("busy apply created .editorconfig (stat err: %v)", statErr)
	}
	content, readErr := os.ReadFile(manifestPath) //nolint:gosec // test-controlled path
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(content) != 0 {
		t.Errorf("busy apply rewrote the manifest: %q", content)
	}
}

// TestConcurrentAppliesWithRetryNoLostUpdates reproduces the issue's ten-way
// apply while retrying busy exit codes. Serialization must retain every target
// file and every manifest entry.
//
//nolint:cyclop // Concurrent worker orchestration plus manifest assertions.
func TestConcurrentAppliesWithRetryNoLostUpdates(t *testing.T) {
	project := setupLockTestProject(t)

	start := make(chan struct{})
	errs := make([]error, lockTestWorkers)
	outputs := make([]string, lockTestWorkers)

	var wg sync.WaitGroup
	for i := 1; i <= lockTestWorkers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			for attempt := 0; attempt < 300; attempt++ {
				output, err := runCLI(applyTarget(index)...)
				outputs[index-1] = output
				if err == nil {
					return
				}
				code, ok := exitCodeOf(err)
				if !ok || code != fileutil.ExitBusy {
					errs[index-1] = fmt.Errorf("apply p%d failed with a non-busy error: %w\n%s", index, err, output)
					return
				}
				// Wait briefly for the current holder to finish, then retry.
				time.Sleep(10 * time.Millisecond)
			}
			errs[index-1] = fmt.Errorf("apply p%d remained busy after retries\n%s", index, outputs[index-1])
		}(i)
	}
	close(start)
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 1; i <= lockTestWorkers; i++ {
		target := filepath.Join(project, "par", fmt.Sprintf("p%d", i), ".editorconfig")
		if _, err := os.Stat(target); err != nil {
			t.Errorf("target file %s missing after concurrent applies: %v", target, err)
		}
	}

	m, _, err := manifest.Load(project)
	if err != nil {
		t.Fatalf("loading final manifest: %v", err)
	}
	seen := make(map[string]bool)
	for _, tmpl := range m.Templates {
		if strings.HasPrefix(tmpl.Path, "par/") {
			if seen[tmpl.Path] {
				t.Errorf("duplicate manifest entry for %s", tmpl.Path)
			}
			seen[tmpl.Path] = true
		}
	}
	if len(seen) != lockTestWorkers {
		t.Errorf("manifest has %d parallel template entries, want %d: %v", len(seen), lockTestWorkers, seen)
	}
	for i := 1; i <= lockTestWorkers; i++ {
		want := fmt.Sprintf("par/p%d", i)
		if !seen[want] {
			t.Errorf("manifest is missing template entry %s", want)
		}
	}
}

// TestConcurrentAppliesWithoutRetryNeverPartiallyMutate verifies losers may exit
// busy but never leave a target file or manifest entry behind.
//
//nolint:cyclop // Concurrent worker orchestration plus per-target assertions.
func TestConcurrentAppliesWithoutRetryNeverPartiallyMutate(t *testing.T) {
	project := setupLockTestProject(t)

	start := make(chan struct{})
	errs := make([]error, lockTestWorkers)
	outputs := make([]string, lockTestWorkers)
	codes := make([]int, lockTestWorkers)

	var wg sync.WaitGroup
	for i := 1; i <= lockTestWorkers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			output, err := runCLI(applyTarget(index)...)
			outputs[index-1] = output
			if err == nil {
				return
			}
			code, ok := exitCodeOf(err)
			if !ok {
				errs[index-1] = fmt.Errorf("apply p%d failed without an exit code: %w\n%s", index, err, output)
				return
			}
			codes[index-1] = code
		}(i)
	}
	close(start)
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	successes := 0
	for i := 1; i <= lockTestWorkers; i++ {
		code := codes[i-1]
		if code == fileutil.ExitBusy {
			target := filepath.Join(project, "par", fmt.Sprintf("p%d", i), ".editorconfig")
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Errorf("busy apply p%d mutated its target (stat err: %v)\n%s", i, err, outputs[i-1])
			}
			continue
		}
		if code != 0 {
			t.Fatalf("apply p%d exit code = %d, want 0 or %d\n%s", i, code, fileutil.ExitBusy, outputs[i-1])
		}
		successes++
		target := filepath.Join(project, "par", fmt.Sprintf("p%d", i), ".editorconfig")
		if _, err := os.Stat(target); err != nil {
			t.Errorf("successful apply p%d did not create its target: %v", i, err)
		}
	}

	m, _, err := manifest.Load(project)
	if err != nil {
		t.Fatalf("loading final manifest: %v", err)
	}
	entries := 0
	for _, tmpl := range m.Templates {
		if strings.HasPrefix(tmpl.Path, "par/") {
			entries++
		}
	}
	if entries != successes {
		t.Errorf("manifest has %d parallel template entries, want %d successful applies", entries, successes)
	}
}

// TestSignalReleasesRootLock verifies the in-process rollback path releases the
// lock before the process exits with the signal status.
//
//nolint:cyclop // Two signals, process orchestration, and lock assertions.
func TestSignalReleasesRootLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal delivery is not available on Windows")
	}

	tests := []struct {
		name string
		sig  os.Signal
		want int
	}{
		{"SIGINT", os.Interrupt, fileutil.ExitInterrupted},
		{"SIGTERM", syscall.SIGTERM, fileutil.ExitTerminated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work := t.TempDir()
			installBlockingGo(t, work)

			project := filepath.Join(work, "project")
			if err := os.MkdirAll(project, fileutil.DirPerms); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, fileutil.ManifestFileName), nil, fileutil.PrivateFilePerms); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/demo\n"), fileutil.PrivateFilePerms); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, "main.go"), []byte("package main\n"), fileutil.PrivateFilePerms); err != nil {
				t.Fatal(err)
			}

			cmd := exec.CommandContext(context.Background(), binaryPath, "apply", "--lang", "go", "--force", ".")
			cmd.Dir = project
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(work, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output

			if err := cmd.Start(); err != nil {
				t.Fatalf("starting scbake: %v", err)
			}
			waitForPath(t, filepath.Join(project, "tidy.started"), tidyTimeout)

			if err := cmd.Process.Signal(tt.sig); err != nil {
				_ = cmd.Process.Kill()
				t.Fatalf("sending %s: %v", tt.sig, err)
			}
			waitErr := waitForCommand(t, cmd, interruptWait)
			exitErr, ok := waitErr.(*exec.ExitError)
			if !ok {
				t.Fatalf("scbake did not fail after %s (err=%v, output=%s)", tt.sig, waitErr, output.String())
			}
			if exitErr.ExitCode() != tt.want {
				t.Errorf("exit code = %d, want %d (output=%s)", exitErr.ExitCode(), tt.want, output.String())
			}

			lock, err := transaction.AcquireRootLock(project)
			if err != nil {
				t.Fatalf("lock was not released after %s: %v", tt.sig, err)
			}
			if err := lock.Release(); err != nil {
				t.Fatalf("releasing probe lock failed: %v", err)
			}
			if _, err := os.Stat(filepath.Join(project, fileutil.InternalDir)); !os.IsNotExist(err) {
				t.Errorf("signal cleanup left .scbake residue (stat err: %v)", err)
			}
		})
	}
}
