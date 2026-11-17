// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/util/fileutil"
)

func findRootCommands(name string) []*cobra.Command {
	var found []*cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == name {
			found = append(found, c)
		}
	}
	return found
}

// TestAcceptUnmanageRegistration pins the CLI surface: one top-level verb each,
// at least one path argument, and help that distinguishes rebaseline from
// detach.
func TestAcceptUnmanageRegistration(t *testing.T) { //nolint:cyclop // table-driven CLI registration assertions
	resetFlags()
	t.Cleanup(resetFlags)

	accepts := findRootCommands("accept")
	if len(accepts) != 1 {
		t.Fatalf("accept registered %d times, want 1", len(accepts))
	}
	unmanages := findRootCommands("unmanage")
	if len(unmanages) != 1 {
		t.Fatalf("unmanage registered %d times, want 1", len(unmanages))
	}

	accept, unmanage := accepts[0], unmanages[0]
	if accept.Use != "accept <path>..." {
		t.Errorf("accept Use = %q", accept.Use)
	}
	if unmanage.Use != "unmanage <path>..." {
		t.Errorf("unmanage Use = %q", unmanage.Use)
	}
	for _, c := range []*cobra.Command{accept, unmanage} {
		if err := c.Args(c, nil); err == nil {
			t.Errorf("%s accepted zero paths", c.Name())
		}
		if err := c.Args(c, []string{"main.go"}); err != nil {
			t.Errorf("%s rejected one path: %v", c.Name(), err)
		}
		if !c.SilenceUsage {
			t.Errorf("%s does not silence usage on runtime errors", c.Name())
		}
	}

	for _, want := range []string{"one-shot", "unmanage", "not rewritten"} {
		if !strings.Contains(accept.Long, want) {
			t.Errorf("accept help missing %q", want)
		}
	}
	for _, want := range []string{"Detach", "apply --force", ".gitignore"} {
		if !strings.Contains(unmanage.Long, want) {
			t.Errorf("unmanage help missing %q", want)
		}
	}
}

// TestAcceptUnmanageLockPolicy: both mutators fail with ErrBusy (exit 75) while
// the root lock is held, and --dry-run stays lock-free.
func TestAcceptUnmanageLockPolicy(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), nil, fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	lock, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("AcquireRootLock: %v", err)
	}
	defer func() { _ = lock.Release() }()
	t.Chdir(root)

	for _, args := range [][]string{{"accept", "main.go"}, {"unmanage", "main.go"}} {
		err := executeCLI(args...)
		if !errors.Is(err, transaction.ErrBusy) {
			t.Fatalf("%v error = %v, want ErrBusy", args, err)
		}
		if code := commandExitCode(err, nil); code != fileutil.ExitBusy {
			t.Errorf("%v exit code = %d, want %d", args, code, fileutil.ExitBusy)
		}
	}

	// Dry-run must not take the lock; it fails later on the untracked path, not
	// with a busy exit.
	err = executeCLI("accept", "--dry-run", "main.go")
	if errors.Is(err, transaction.ErrBusy) {
		t.Fatalf("accept --dry-run contended for the root lock: %v", err)
	}
	if code := commandExitCode(err, nil); code == fileutil.ExitBusy {
		t.Fatalf("accept --dry-run mapped to exit 75")
	}
}
