// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// issue151BlockedEnv lists variables that could smuggle a Git identity,
// repository, or discovery boundary into a regression subprocess. The tests
// build their environment from os.Environ() (so PATH, SYSTEMROOT, and similar
// platform state survive) after removing these keys.
var issue151BlockedEnv = map[string]bool{
	"GIT_AUTHOR_NAME":         true,
	"GIT_AUTHOR_EMAIL":        true,
	"GIT_COMMITTER_NAME":      true,
	"GIT_COMMITTER_EMAIL":     true,
	"GIT_AUTHOR_DATE":         true,
	"GIT_COMMITTER_DATE":      true,
	"EMAIL":                   true,
	"GIT_DIR":                 true,
	"GIT_WORK_TREE":           true,
	"GIT_INDEX_FILE":          true,
	"GIT_OBJECT_DIRECTORY":    true,
	"GIT_CONFIG_GLOBAL":       true,
	"GIT_CONFIG_SYSTEM":       true,
	"GIT_CONFIG_NOSYSTEM":     true,
	"GIT_CONFIG_COUNT":        true,
	"GIT_CONFIG_PARAMETERS":   true,
	"GIT_CEILING_DIRECTORIES": true,
	"XDG_CONFIG_HOME":         true,
	"XDG_CONFIG_DIRS":         true,
}

// issue151Env returns a filtered environment with a fresh HOME. workDir is the
// top of the fixture; GIT_CEILING_DIRECTORIES names its parent so a repository
// above the fixture (including one at the parent itself) can never be
// discovered. identity adds only the four env-var identity values.
func issue151Env(t *testing.T, workDir string, identity bool) []string {
	t.Helper()

	home := t.TempDir()
	env := make([]string, 0, len(os.Environ())+8)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || issue151BlockedEnv[key] {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"),
		"XDG_CACHE_HOME="+filepath.Join(home, "cache"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(workDir),
	)
	if identity {
		env = append(env,
			"GIT_AUTHOR_NAME=Issue 151",
			"GIT_AUTHOR_EMAIL=issue151@example.com",
			"GIT_COMMITTER_NAME=Issue 151",
			"GIT_COMMITTER_EMAIL=issue151@example.com",
		)
	}
	return env
}

// runIssue151 executes the compiled scbake binary in dir and returns combined
// stdout+stderr.
func runIssue151(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()

	//nolint:gosec // test-controlled binary and arguments
	cmd := exec.CommandContext(context.Background(), binaryPath, args...)
	cmd.Dir = dir
	cmd.Env = env
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

// runGit151 executes git in dir with the test process environment (identity
// setup for fixtures only). It must not be used to run the CLI.
func runGit151(t *testing.T, dir string, args ...string) string {
	t.Helper()

	//nolint:gosec // fixed test helper
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// issue151FixtureRepo initializes a repository with a local-only identity and
// one commit. The CLI environment has no identity, so a naive identity
// preflight run inside a subdirectory can still see this local config; that is
// exactly the false negative this fixture must expose.
func issue151FixtureRepo(t *testing.T, workDir string) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(workDir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit151(t, repo, "init", "-q")
	runGit151(t, repo, "config", "user.name", "Parent Local")
	runGit151(t, repo, "config", "user.email", "parent@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("parent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit151(t, repo, "add", "README.md")
	runGit151(t, repo, "commit", "-q", "-m", "parent commit")
	return repo
}

// TestIssue151NewFailsBeforeCreatingDirectory is the destructive-failure
// regression: with no identity, `new --with editorconfig,git` must fail before
// the first write, leaving neither the project directory nor `.scbake` state.
// Base creates the directory, writes files, then fails at the commit.
func TestIssue151NewFailsBeforeCreatingDirectory(t *testing.T) {
	work := t.TempDir()
	env := issue151Env(t, work, false)

	output, err := runIssue151(t, work, env, "new", "demo", "--with", "editorconfig,git")
	if err == nil {
		t.Fatalf("new succeeded without a Git identity:\n%s", output)
	}
	if strings.Contains(output, "Creating directory") {
		t.Errorf("new reported creating the project directory before detecting the missing identity:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(work, "demo")); !os.IsNotExist(statErr) {
		t.Errorf("new left the project directory behind (stat error: %v):\n%s", statErr, output)
	}
	if _, statErr := os.Lstat(filepath.Join(work, ".scbake")); !os.IsNotExist(statErr) {
		t.Errorf("new left .scbake transaction residue behind (stat error: %v):\n%s", statErr, output)
	}
	entries, readErr := os.ReadDir(work)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("new wrote into the work directory before failing: %v", names)
	}
	if !strings.Contains(strings.ToLower(output), "identity") && !strings.Contains(output, "user.email") {
		t.Errorf("error does not explain the missing Git identity:\n%s", output)
	}
}

// TestIssue151NewDryRunFailsMissingIdentity pins dry-run honesty: a preview of
// a run that cannot commit must fail and must not print the commit plan. Base
// exits 0 and prints "[DRY RUN] Create initial commit".
func TestIssue151NewDryRunFailsMissingIdentity(t *testing.T) {
	work := t.TempDir()
	env := issue151Env(t, work, false)

	output, err := runIssue151(t, work, env, "new", "demo", "--with", "git", "--dry-run")
	if err == nil {
		t.Fatalf("dry run succeeded without a Git identity:\n%s", output)
	}
	if n := strings.Count(output, "[DRY RUN] Create initial commit"); n != 0 {
		t.Errorf("dry run advertised the %d commit task(s) it cannot perform:\n%s", n, output)
	}
	if _, statErr := os.Stat(filepath.Join(work, "demo")); !os.IsNotExist(statErr) {
		t.Errorf("dry run created the project directory (stat error: %v)", statErr)
	}
	if !strings.Contains(strings.ToLower(output), "identity") && !strings.Contains(output, "user.email") {
		t.Errorf("dry run error does not explain the missing Git identity:\n%s", output)
	}
}

// TestIssue151ApplySkipsNestedRepo is the decisive nested test. The CLI
// environment has no identity; the only identity is repo-local config, which a
// fresh nested repository does not inherit. A naive identity-only fix would
// probe `git var` in svc (seeing the parent config), pass, then create
// svc/.git and fail to commit. The fixed behaviour is to skip the template,
// commit nothing, and keep the root manifest as the discovery root.
func TestIssue151ApplySkipsNestedRepo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX-relative Git behaviour")
	}
	work := t.TempDir()
	repo := issue151FixtureRepo(t, work)
	svc := filepath.Join(repo, "svc")
	if err := os.MkdirAll(svc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc, "app.txt"), []byte("pre-existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scbake.toml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	env := issue151Env(t, work, false)
	output, err := runIssue151(t, repo, env, "apply", "--with", "git", "svc")
	if err != nil {
		t.Fatalf("nested apply failed: %v\n%s", err, output)
	}
	if _, statErr := os.Lstat(filepath.Join(svc, ".git")); !os.IsNotExist(statErr) {
		t.Errorf("nested apply created svc/.git (stat error: %v):\n%s", statErr, output)
	}
	if !strings.Contains(output, "Skipping template 'git'") {
		t.Errorf("nested apply did not print the skip notice:\n%s", output)
	}
	if !strings.Contains(output, "inside a Git work tree") {
		t.Errorf("skip notice does not explain that the target is inside a work tree:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(svc, "scbake.toml")); !os.IsNotExist(statErr) {
		t.Errorf("nested apply wrote a local manifest instead of using the root (stat error: %v)", statErr)
	}

	tracked := runGit151(t, repo, "ls-files")
	if strings.Contains(tracked, "svc/app.txt") || strings.Contains(tracked, "app.txt") {
		t.Errorf("pre-existing svc/app.txt was committed into the parent repository:\n%s", tracked)
	}
	if commits := strings.TrimSpace(runGit151(t, repo, "rev-list", "--count", "HEAD")); commits != "1" {
		t.Errorf("nested apply created a commit in the parent repository (count=%q):\n%s", commits, output)
	}

	// Follow-up: with no nested .git, discovery still selects the parent
	// manifest, so a scoped apply updates repo/scbake.toml and not svc's.
	followUp, followErr := runIssue151(t, repo, env, "apply", "--with", "makefile", "svc")
	if followErr != nil {
		t.Fatalf("follow-up apply failed: %v\n%s", followErr, followUp)
	}
	rootManifest, readErr := os.ReadFile(filepath.Join(repo, "scbake.toml")) //nolint:gosec // test-controlled path
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(rootManifest), "makefile") {
		t.Errorf("root manifest was not updated by the follow-up apply: %q", rootManifest)
	}
	if _, statErr := os.Stat(filepath.Join(svc, "Makefile")); statErr != nil {
		t.Errorf("follow-up apply did not write svc/Makefile: %v", statErr)
	}
}

// TestIssue151NewSkipsNestedRepo covers `new` inside an existing repository
// with a full identity available. Base exits 0 but creates nested/.git; the fix
// creates the project without a nested repository and prints the notice.
func TestIssue151NewSkipsNestedRepo(t *testing.T) {
	work := t.TempDir()
	repo := issue151FixtureRepo(t, work)

	env := issue151Env(t, work, true)
	output, err := runIssue151(t, repo, env, "new", "nested", "--with", "git")
	if err != nil {
		t.Fatalf("new inside a repository failed: %v\n%s", err, output)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, "nested", ".git")); !os.IsNotExist(statErr) {
		t.Errorf("new created a nested .git inside the parent repository (stat error: %v):\n%s", statErr, output)
	}
	if !strings.Contains(output, "Skipping template 'git'") {
		t.Errorf("new did not print the skip notice:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "nested", "scbake.toml")); statErr != nil {
		t.Errorf("new did not create the project without the git template: %v", statErr)
	}
}

// TestIssue151DryRunApplyShowsSkip keeps the nested dry-run plan honest: no
// identity is configured, yet the run must succeed because no repository will
// be created, print the skip notice, and plan no git init. Base prints the git
// plan.
func TestIssue151DryRunApplyShowsSkip(t *testing.T) {
	work := t.TempDir()
	repo := issue151FixtureRepo(t, work)
	if err := os.MkdirAll(filepath.Join(repo, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scbake.toml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	env := issue151Env(t, work, false)
	output, err := runIssue151(t, repo, env, "apply", "--with", "git", "svc", "--dry-run")
	if err != nil {
		t.Fatalf("nested dry-run apply failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "Initialize Git repository") {
		t.Errorf("nested dry run still plans git init:\n%s", output)
	}
	if strings.Contains(output, "Create initial commit") {
		t.Errorf("nested dry run still plans an initial commit:\n%s", output)
	}
	if !strings.Contains(output, "Skipping template 'git'") {
		t.Errorf("nested dry run did not print the skip notice:\n%s", output)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, "svc", ".git")); !os.IsNotExist(statErr) {
		t.Errorf("nested dry run created svc/.git (stat error: %v)", statErr)
	}
}
