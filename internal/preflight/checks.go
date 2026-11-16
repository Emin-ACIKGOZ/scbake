// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package preflight

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
	"time"
)

const (
	gitCommand        = "git"
	gitProbeTimeout   = 10 * time.Second
	gitAuthorIdent    = "GIT_AUTHOR_IDENT"
	gitCommitterIdent = "GIT_COMMITTER_IDENT"
)

const (
	gitSkipExistingRepo   = "target already contains a .git entry"
	gitSkipInsideWorkTree = "target is already inside a Git work tree"
)

// CheckBinaries ensures all required binaries are in the user's $PATH.
func CheckBinaries(binaries ...string) error {
	for _, bin := range binaries {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("'%s' command not found in $PATH. Please install it to continue", bin)
		}
	}
	return nil
}

// GitSkipReason reports why the git template must not run in targetPath, or ""
// when it may run. A target with a .git entry is skipped without invoking Git
// (SCB-037): its repository config is untrusted. A target inside a work tree is
// skipped because initializing a repository there would create a nested
// repository and bound later root discovery to that subdirectory.
//
// An empty targetPath means the caller has no path to inspect; it is not
// treated as "." so direct handler callers keep their existing behaviour.
func GitSkipReason(targetPath string) string {
	if targetPath == "" {
		return ""
	}
	if _, err := os.Lstat(filepath.Join(targetPath, fileutil.GitDir)); err == nil {
		return gitSkipExistingRepo
	}
	if InsideWorkTree(targetPath) {
		return gitSkipInsideWorkTree
	}
	return ""
}

// InsideWorkTree reports whether dir (or its nearest existing ancestor) is
// inside a Git work tree. A non-existing target is probed through its nearest
// existing ancestor so a dry-run `new` previews the same decision as the real
// run. Git plumbing (`rev-parse`) is read-only and does not run hooks, filters,
// or fsmonitor programs.
func InsideWorkTree(dir string) bool {
	probe := nearestExistingDir(dir)
	if probe == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitProbeTimeout)
	defer cancel()

	//nolint:gosec // fixed read-only git plumbing command; probe is a validated directory
	cmd := exec.CommandContext(ctx, gitCommand, "-C", probe, "rev-parse", "--is-inside-work-tree")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.TrimSpace(stdout.String()) == "true"
}

// CheckGitIdentity verifies that Git can resolve both the author and the
// committer identity for a repository created at dir. `git var` is the
// authoritative check: it honors environment variables, global/system config,
// and include files exactly like `git commit` will. Checking user.name and
// user.email separately is wrong: Git may auto-detect a name when only
// user.email is configured, and author and committer do not fall back to each
// other.
func CheckGitIdentity(dir string) error {
	if err := CheckBinaries(gitCommand); err != nil {
		return err
	}
	probe := nearestExistingDir(dir)
	if probe == "" {
		probe = "."
	}

	failures := make([]string, 0, 2)
	for _, ident := range []string{gitAuthorIdent, gitCommitterIdent} {
		if detail := gitIdentityFailure(probe, ident); detail != "" {
			failures = append(failures, ident+": "+detail)
		}
	}
	if len(failures) == 0 {
		return nil
	}

	return fmt.Errorf("git cannot resolve the commit identity needed to create a repository (%s). "+
		"Set user.name and user.email, e.g. `git config --global user.name \"Your Name\"` and "+
		"`git config --global user.email \"you@example.com\"`, or export GIT_AUTHOR_NAME/GIT_AUTHOR_EMAIL "+
		"and GIT_COMMITTER_NAME/GIT_COMMITTER_EMAIL, then retry", strings.Join(failures, "; "))
}

// gitIdentityFailure runs one `git var` identity probe and returns a diagnostic
// when it fails or produces no identity. An empty return means success.
func gitIdentityFailure(probe, ident string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitProbeTimeout)
	defer cancel()

	//nolint:gosec // fixed read-only git plumbing command; probe is a validated directory
	cmd := exec.CommandContext(ctx, gitCommand, "-C", probe, "var", ident)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("timed out after %s", gitProbeTimeout)
	}
	if err != nil {
		if detail := firstNonEmptyLine(stderr.String()); detail != "" {
			return detail
		}
		return err.Error()
	}
	if strings.TrimSpace(stdout.String()) == "" {
		return "no identity output"
	}
	return ""
}

// firstNonEmptyLine returns the first non-blank, trimmed line of output.
func firstNonEmptyLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// nearestExistingDir resolves path to the nearest existing directory. It
// returns "" only when no ancestor can be resolved. A non-existing dry-run
// target therefore probes the closest real directory, which shares its Git
// configuration and ancestor repository chain.
func nearestExistingDir(path string) string {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	for {
		info, statErr := os.Stat(abs)
		if statErr == nil {
			if info.IsDir() {
				return abs
			}
			return filepath.Dir(abs)
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		abs = parent
	}
}
