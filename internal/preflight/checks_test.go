// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package preflight

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// isolateGitEnv gives the test process a private Git configuration and removes
// every inherited identity/repository variable. Tests that need a specific
// value set it afterwards with t.Setenv.
func isolateGitEnv(t *testing.T) string {
	t.Helper()

	for _, name := range []string{
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_CEILING_DIRECTORIES",
		"GIT_AUTHOR_NAME",
		"GIT_AUTHOR_EMAIL",
		"GIT_COMMITTER_NAME",
		"GIT_COMMITTER_EMAIL",
		"GIT_AUTHOR_DATE",
		"GIT_COMMITTER_DATE",
		"EMAIL",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_PARAMETERS",
	} {
		clearEnv(t, name)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	global := filepath.Join(home, "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	return global
}

func clearEnv(t *testing.T, name string) {
	t.Helper()
	if value, ok := os.LookupEnv(name); ok {
		t.Cleanup(func() {
			_ = os.Setenv(name, value)
		})
	}
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func execGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	//nolint:gosec // fixed test helper
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func setIdentity(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Issue 151")
	t.Setenv("GIT_AUTHOR_EMAIL", "issue151@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Issue 151")
	t.Setenv("GIT_COMMITTER_EMAIL", "issue151@example.com")
}

// writeGlobalConfig replaces the fresh global config file with a [user]
// section. useConfigOnly stops Git from guessing a missing name or email from
// the account, host, or environment, so the identity matrix does not depend on
// the CI machine. extra lines are written verbatim inside the section.
func writeGlobalConfig(t *testing.T, global string, useConfigOnly bool, extra ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("[user]\n")
	if useConfigOnly {
		b.WriteString("\tuseConfigOnly = true\n")
	}
	for _, line := range extra {
		b.WriteString("\t" + line + "\n")
	}
	if err := os.WriteFile(global, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCheckGitIdentity covers the identity matrix. `git var` is authoritative;
// these cases are the ones where checking `git config user.name/user.email`
// would disagree with `git commit`. Partial identities are pinned with
// user.useConfigOnly so Git cannot auto-fill the missing half on some hosts.
func TestCheckGitIdentity(t *testing.T) { //nolint:cyclop // assertion-heavy identity matrix
	requireGit(t)

	t.Run("no identity", func(t *testing.T) {
		global := isolateGitEnv(t)
		writeGlobalConfig(t, global, true)
		err := CheckGitIdentity(t.TempDir())
		if err == nil {
			t.Fatal("CheckGitIdentity succeeded without an identity")
		}
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "identity") || !strings.Contains(lower, "user.email") {
			t.Errorf("error does not explain the missing identity: %v", err)
		}
	})

	t.Run("both env identities", func(t *testing.T) {
		isolateGitEnv(t)
		setIdentity(t)
		if err := CheckGitIdentity(t.TempDir()); err != nil {
			t.Fatalf("CheckGitIdentity with a full environment identity: %v", err)
		}
	})

	t.Run("author only", func(t *testing.T) {
		isolateGitEnv(t)
		t.Setenv("GIT_AUTHOR_NAME", "Issue 151")
		t.Setenv("GIT_AUTHOR_EMAIL", "issue151@example.com")
		err := CheckGitIdentity(t.TempDir())
		if err == nil {
			t.Fatal("CheckGitIdentity succeeded with only an author identity")
		}
		if !strings.Contains(err.Error(), gitCommitterIdent) {
			t.Errorf("error does not name %s: %v", gitCommitterIdent, err)
		}
	})

	t.Run("committer only", func(t *testing.T) {
		isolateGitEnv(t)
		t.Setenv("GIT_COMMITTER_NAME", "Issue 151")
		t.Setenv("GIT_COMMITTER_EMAIL", "issue151@example.com")
		err := CheckGitIdentity(t.TempDir())
		if err == nil {
			t.Fatal("CheckGitIdentity succeeded with only a committer identity")
		}
		if !strings.Contains(err.Error(), gitAuthorIdent) {
			t.Errorf("error does not name %s: %v", gitAuthorIdent, err)
		}
	})

	t.Run("email-only global config", func(t *testing.T) {
		global := isolateGitEnv(t)
		// useConfigOnly denies Git the account/host fallback, so an email
		// without a name must fail just like it does on a clean CI machine.
		writeGlobalConfig(t, global, true, "email = only@example.com")
		if err := CheckGitIdentity(t.TempDir()); err == nil {
			t.Fatal("CheckGitIdentity succeeded with an email but no name")
		}
	})

	t.Run("name-only global config", func(t *testing.T) {
		global := isolateGitEnv(t)
		// useConfigOnly denies Git the host-based fallback email.
		writeGlobalConfig(t, global, true, "name = Only Name")
		if err := CheckGitIdentity(t.TempDir()); err == nil {
			t.Fatal("CheckGitIdentity succeeded with a name but no email")
		}
	})

	t.Run("both global config", func(t *testing.T) {
		global := isolateGitEnv(t)
		writeGlobalConfig(t, global, true, "name = Both Name", "email = both@example.com")
		if err := CheckGitIdentity(t.TempDir()); err != nil {
			t.Fatalf("CheckGitIdentity with a complete global config: %v", err)
		}
	})

	t.Run("env-only EMAIL is accepted", func(t *testing.T) {
		global := isolateGitEnv(t)
		// EMAIL supplies the email; the names come from the environment. The
		// global config stays empty and auto-detection is left enabled so Git
		// reads EMAIL rather than a host-derived address.
		writeGlobalConfig(t, global, false)
		t.Setenv("GIT_AUTHOR_NAME", "Issue 151")
		t.Setenv("GIT_COMMITTER_NAME", "Issue 151")
		t.Setenv("EMAIL", "env@example.com")
		if err := CheckGitIdentity(t.TempDir()); err != nil {
			t.Fatalf("EMAIL env var should supply the email: %v", err)
		}
	})

	t.Run("invalid author date is reported as such", func(t *testing.T) {
		isolateGitEnv(t)
		setIdentity(t)
		t.Setenv("GIT_AUTHOR_DATE", "not-a-date")
		err := CheckGitIdentity(t.TempDir())
		if err == nil {
			t.Fatal("CheckGitIdentity succeeded with an invalid GIT_AUTHOR_DATE")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "date") {
			t.Errorf("error misreports the invalid date as missing config: %v", err)
		}
	})

	t.Run("empty stdout is a failure", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX fake git script")
		}
		isolateGitEnv(t)
		binDir := t.TempDir()
		writeFakeGit(t, binDir, "", 0)
		t.Setenv("PATH", binDir)
		err := CheckGitIdentity(t.TempDir())
		if err == nil {
			t.Fatal("CheckGitIdentity accepted empty stdout with exit 0")
		}
		if !strings.Contains(err.Error(), "no identity output") {
			t.Errorf("error does not report empty identity output: %v", err)
		}
	})

	t.Run("missing git binary", func(t *testing.T) {
		isolateGitEnv(t)
		t.Setenv("PATH", t.TempDir())
		err := CheckGitIdentity(t.TempDir())
		if err == nil {
			t.Fatal("CheckGitIdentity succeeded without a git binary")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error does not report the missing binary: %v", err)
		}
	})
}

// writeFakeGit writes a POSIX shell fake git in dir. body is the shell body;
// the script exits with exitCode when it falls through.
func writeFakeGit(t *testing.T, dir, body string, exitCode int) string {
	t.Helper()
	path := filepath.Join(dir, "git")
	script := "#!/bin/sh\n" + body + "\nexit " + strconv.Itoa(exitCode) + "\n"
	//nolint:gosec // the fake git must be executable for exec to invoke it
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// shellQuote single-quotes s for /bin/sh so the fake-git body can create a
// marker without relying on external commands such as touch.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TestGitSkipReason covers the predicates that decide whether a repository
// may be initialized. Cases marked "documented edge" deliberately follow the
// shipped fail-closed (or fail-open) behavior; they are pinned so a future
// change has to make a conscious choice.
func TestGitSkipReason(t *testing.T) { //nolint:cyclop // one branch per skip predicate
	requireGit(t)

	t.Run("empty target does not probe cwd", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX fake git script")
		}
		isolateGitEnv(t)
		marker := filepath.Join(t.TempDir(), "invoked.marker")
		binDir := t.TempDir()
		writeFakeGit(t, binDir, ": > "+shellQuote(marker), 1)
		t.Setenv("PATH", binDir)

		if reason := GitSkipReason(""); reason != "" {
			t.Errorf("GitSkipReason(\"\") = %q, want empty", reason)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Error("GitSkipReason(\"\") invoked git")
		}
	})

	t.Run("plain directory", func(t *testing.T) {
		isolateGitEnv(t)
		root := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
		if reason := GitSkipReason(root); reason != "" {
			t.Errorf("GitSkipReason(plain dir) = %q, want empty", reason)
		}
	})

	t.Run(".git directory short-circuits without git", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX fake git script")
		}
		isolateGitEnv(t)
		target := t.TempDir()
		if err := os.MkdirAll(filepath.Join(target, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(t.TempDir(), "invoked.marker")
		binDir := t.TempDir()
		writeFakeGit(t, binDir, ": > "+shellQuote(marker), 1)
		t.Setenv("PATH", binDir)

		if reason := GitSkipReason(target); reason != gitSkipExistingRepo {
			t.Errorf("GitSkipReason(.git dir) = %q, want %q", reason, gitSkipExistingRepo)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Error("GitSkipReason invoked git for a target containing .git")
		}
	})

	t.Run(".git file", func(t *testing.T) {
		isolateGitEnv(t)
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, ".git"), []byte("gitdir: elsewhere\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if reason := GitSkipReason(target); reason != gitSkipExistingRepo {
			t.Errorf("GitSkipReason(.git file) = %q, want %q", reason, gitSkipExistingRepo)
		}
	})

	t.Run("dangling .git symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires privileges on Windows")
		}
		isolateGitEnv(t)
		target := t.TempDir()
		if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(target, ".git")); err != nil {
			t.Fatal(err)
		}
		if reason := GitSkipReason(target); reason != gitSkipExistingRepo {
			t.Errorf("GitSkipReason(dangling .git) = %q, want %q", reason, gitSkipExistingRepo)
		}
	})

	t.Run("subdirectory of a work tree", func(t *testing.T) {
		isolateGitEnv(t)
		root := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
		repo := filepath.Join(root, "repo")
		if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o750); err != nil {
			t.Fatal(err)
		}
		execGit(t, repo, "init", "-q")
		if reason := GitSkipReason(filepath.Join(repo, "sub")); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(subdir) = %q, want %q", reason, gitSkipInsideWorkTree)
		}
	})

	t.Run("nonexistent target under a work tree", func(t *testing.T) {
		isolateGitEnv(t)
		root := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
		repo := filepath.Join(root, "repo")
		if err := os.MkdirAll(repo, 0o750); err != nil {
			t.Fatal(err)
		}
		execGit(t, repo, "init", "-q")
		missing := filepath.Join(repo, "not", "created", "yet")
		if reason := GitSkipReason(missing); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(missing under repo) = %q, want %q", reason, gitSkipInsideWorkTree)
		}
	})

	t.Run("GIT_CEILING_DIRECTORIES hides the work tree from git", func(t *testing.T) {
		isolateGitEnv(t)
		root := t.TempDir()
		repo := filepath.Join(root, "repo")
		if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o750); err != nil {
			t.Fatal(err)
		}
		execGit(t, repo, "init", "-q")
		// Git stops below repo and cannot see repo/.git, but discovery still
		// treats repo as the boundary. The marker fallback must skip.
		t.Setenv("GIT_CEILING_DIRECTORIES", repo)
		if reason := GitSkipReason(filepath.Join(repo, "sub")); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(ceiling-hidden subdir) = %q, want %q", reason, gitSkipInsideWorkTree)
		}
		// A `new` target does not exist yet; the fallback must still find the
		// boundary by walking from its nearest existing ancestor.
		missing := filepath.Join(repo, "not", "created", "yet")
		if reason := GitSkipReason(missing); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(ceiling-hidden missing target) = %q, want %q", reason, gitSkipInsideWorkTree)
		}
	})

	t.Run("empty ancestor .git", func(t *testing.T) {
		isolateGitEnv(t)
		root := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", root)
		boundary := filepath.Join(root, "broken")
		if err := os.MkdirAll(filepath.Join(boundary, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(boundary, "sub")
		if err := os.MkdirAll(sub, 0o750); err != nil {
			t.Fatal(err)
		}
		if reason := GitSkipReason(sub); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(empty ancestor .git) = %q, want %q", reason, gitSkipInsideWorkTree)
		}
		missing := filepath.Join(boundary, "not", "created", "yet")
		if reason := GitSkipReason(missing); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(nonexistent target under empty ancestor .git) = %q, want %q", reason, gitSkipInsideWorkTree)
		}
	})

	t.Run("corrupt ancestor .git file", func(t *testing.T) {
		isolateGitEnv(t)
		root := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", root)
		boundary := filepath.Join(root, "broken")
		if err := os.MkdirAll(boundary, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(boundary, ".git"), []byte("not a gitdir\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(boundary, "sub")
		if err := os.MkdirAll(sub, 0o750); err != nil {
			t.Fatal(err)
		}
		if reason := GitSkipReason(sub); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(corrupt ancestor .git) = %q, want %q", reason, gitSkipInsideWorkTree)
		}
	})

	t.Run("dubious ownership error falls back to the marker", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX fake git script")
		}
		isolateGitEnv(t)
		root := t.TempDir()
		repo := filepath.Join(root, "repo")
		if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o750); err != nil {
			t.Fatal(err)
		}
		execGit(t, repo, "init", "-q")
		binDir := t.TempDir()
		writeFakeGit(t, binDir, `echo "fatal: detected dubious ownership in repository" >&2`, 128)
		t.Setenv("PATH", binDir)
		if reason := GitSkipReason(filepath.Join(repo, "sub")); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(dubious ownership) = %q, want %q", reason, gitSkipInsideWorkTree)
		}
	})

	t.Run("linked worktree", func(t *testing.T) {
		isolateGitEnv(t)
		root := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
		repo := filepath.Join(root, "repo")
		if err := os.MkdirAll(repo, 0o750); err != nil {
			t.Fatal(err)
		}
		execGit(t, repo, "init", "-q")
		execGit(t, repo, "config", "user.name", "Issue 151")
		execGit(t, repo, "config", "user.email", "issue151@example.com")
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		execGit(t, repo, "add", "README.md")
		execGit(t, repo, "commit", "-q", "-m", "base")

		wt := filepath.Join(root, "wt")
		execGit(t, repo, "worktree", "add", "-q", wt)
		// A linked worktree root has a `.git` file, so the Lstat short-circuit
		// reports an existing repository before Git is invoked.
		if reason := GitSkipReason(wt); reason != gitSkipExistingRepo {
			t.Errorf("GitSkipReason(linked worktree root) = %q, want %q", reason, gitSkipExistingRepo)
		}
		sub := filepath.Join(wt, "sub")
		if err := os.MkdirAll(sub, 0o750); err != nil {
			t.Fatal(err)
		}
		if reason := GitSkipReason(sub); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(linked worktree subdir) = %q, want %q", reason, gitSkipInsideWorkTree)
		}
	})

	t.Run("bare repository is a documented edge", func(t *testing.T) {
		isolateGitEnv(t)
		root := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
		bare := filepath.Join(root, "bare.git")
		execGit(t, root, "init", "--bare", "-q", bare)
		if reason := GitSkipReason(bare); reason != "" {
			t.Errorf("GitSkipReason(bare repo) = %q, want empty (documented false-negative edge)", reason)
		}
	})

	t.Run("GIT_DIR pointing at a repository", func(t *testing.T) {
		isolateGitEnv(t)
		root := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
		repo := filepath.Join(root, "repo")
		if err := os.MkdirAll(repo, 0o750); err != nil {
			t.Fatal(err)
		}
		execGit(t, repo, "init", "-q")
		plain := filepath.Join(root, "plain")
		if err := os.MkdirAll(plain, 0o750); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_DIR", filepath.Join(repo, ".git"))
		if reason := GitSkipReason(plain); reason != gitSkipInsideWorkTree {
			t.Errorf("GitSkipReason(GIT_DIR set) = %q, want %q (documented fail-closed)", reason, gitSkipInsideWorkTree)
		}
	})
}
