// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/util/fileutil"
)

// requireStagedPaths fails unless git's index contains every requested path.
func requireStagedPaths(t *testing.T, project string, paths ...string) {
	t.Helper()

	staged := gitOutput(t, project, "ls-files", "--cached")
	lines := strings.Split(strings.TrimSpace(staged), "\n")
	for _, want := range paths {
		found := false
		for _, line := range lines {
			if line == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%q is not staged; staged paths:\n%s", want, staged)
		}
	}
}

// TestCloneMergeParity is the #143 acceptance check: a clone that carries the
// committed .scbake-base ancestor must merge a disjoint local edit and template
// change exactly like the original checkout, with no .scbake-new artifact. A
// full clone and a shallow --depth 1 clone are both covered.
func TestCloneMergeParity(t *testing.T) { //nolint:cyclop // assertion-heavy clone regression test
	requireGit(t)

	for _, tt := range []struct {
		name      string
		cloneArgs []string
	}{
		{name: "full clone"},
		{name: "depth 1 clone", cloneArgs: []string{"--depth", "1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			work := t.TempDir()
			t.Chdir(work)

			overrideDir := filepath.Join(work, "overrides")
			if err := os.MkdirAll(overrideDir, 0o750); err != nil {
				t.Fatalf("create override dir: %v", err)
			}
			mustWrite(t, filepath.Join(overrideDir, "makefile.tpl"), "alpha\nshared\nomega\n")

			out, err := runCLI("new", "proj", "--with", "git,makefile", "--template-dir", overrideDir)
			if err != nil {
				t.Fatalf("new failed: %v\n%s", err, out)
			}
			project := filepath.Join(work, "proj")

			// The initial commit must carry the pristine base, not leave it
			// untracked for the clone to miss.
			tracked := gitOutput(t, project, "ls-files", "--cached", ".scbake-base")
			if !strings.Contains(tracked, ".scbake-base/Makefile") {
				t.Fatalf("initial commit does not track the committed base:\n%s", tracked)
			}
			if strings.Contains(tracked, ".scbake/base") {
				t.Fatalf("initial commit tracks the legacy base:\n%s", tracked)
			}

			clone := filepath.Join(work, "clone")
			src := "file://" + filepath.ToSlash(project)
			cloneArgs := append([]string{"clone"}, tt.cloneArgs...)
			cloneArgs = append(cloneArgs, src, clone)
			gitOutput(t, work, cloneArgs...)

			if _, err := os.Stat(filepath.Join(clone, fileutil.CommittedBaseDir, "Makefile")); err != nil {
				t.Fatalf("clone is missing the committed base: %v", err)
			}
			if _, err := os.Stat(filepath.Join(clone, fileutil.InternalDir, fileutil.BaseDir)); !os.IsNotExist(err) {
				t.Fatalf("clone contains the legacy .scbake/base tree (stat err: %v)", err)
			}

			// A non-overlapping local edit plus a template change.
			mustWrite(t, filepath.Join(clone, "Makefile"), "alpha-local\nshared\nomega\n")
			mustWrite(t, filepath.Join(overrideDir, "makefile.tpl"), "alpha\nshared\nomega-template\n")

			t.Chdir(clone)
			out, err = runCLI("update", "--template-dir", overrideDir)
			if err != nil {
				t.Fatalf("update in clone failed: %v\n%s", err, out)
			}

			if got, want := mustRead(t, filepath.Join(clone, "Makefile")),
				"alpha-local\nshared\nomega-template\n"; got != want {
				t.Fatalf("clone merge = %q, want %q", got, want)
			}
			if _, err := os.Stat(filepath.Join(clone, "Makefile.scbake-new")); !os.IsNotExist(err) {
				t.Fatalf("clone merge produced an artifact (stat err: %v)", err)
			}
		})
	}
}

// TestHostileIgnoreStaging proves the root .gitignore re-include block keeps the
// committed base and the rules file stageable under broad root patterns and a
// hostile global excludes file. gitOutput isolates system/global Git config, so
// only the explicit -c core.excludesFile and the repository's own rules apply.
func TestHostileIgnoreStaging(t *testing.T) {
	requireGit(t)

	for _, tt := range []struct {
		name       string
		prepend    string
		globalStar bool
	}{
		{name: "root dotstar and out", prepend: ".*\n*.out\n"},
		{name: "global star", globalStar: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			work := t.TempDir()
			t.Chdir(work)

			out, err := runCLI("new", "proj", "--lang", "go", "--with", "makefile")
			if err != nil {
				t.Fatalf("new failed: %v\n%s", err, out)
			}
			project := filepath.Join(work, "proj")

			gitignorePath := filepath.Join(project, fileutil.GitIgnore)
			if tt.prepend != "" {
				existing := mustRead(t, gitignorePath)
				// The hostile patterns must precede the block so the block is
				// last and can override them.
				mustWrite(t, gitignorePath, tt.prepend+existing)
			}

			gitOutput(t, project, "init")
			addArgs := []string{"add", "."}
			if tt.globalStar {
				excludes := filepath.Join(work, "global-excludes")
				mustWrite(t, excludes, "*\n")
				addArgs = []string{"-c", "core.excludesFile=" + excludes, "add", "."}
			}
			gitOutput(t, project, addArgs...)

			requireStagedPaths(
				t,
				project,
				fileutil.GitIgnore,
				".scbake-base/Makefile",
				".scbake-base/.gitignore.scbake-shadow",
			)
		})
	}
}

// TestHostilePatternAfterBlockWins documents the known block-order limitation:
// scbake appends missing lines but does not move an existing block, so a
// hostile pattern placed after it overrides the re-includes and the base is not
// staged. This matches README/ARCHITECTURE; a reordered file needs manual
// repair.
func TestHostilePatternAfterBlockWins(t *testing.T) {
	requireGit(t)

	work := t.TempDir()
	t.Chdir(work)

	out, err := runCLI("new", "proj", "--lang", "go", "--with", "makefile")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	project := filepath.Join(work, "proj")

	gitignorePath := filepath.Join(project, fileutil.GitIgnore)
	blockContent := mustRead(t, gitignorePath)
	reordered := blockContent + "*\n"
	mustWrite(t, gitignorePath, reordered)

	gitOutput(t, project, "init")
	ignoreDetail := gitOutput(t, project, "check-ignore", "-v", ".scbake-base/Makefile")
	if !strings.Contains(ignoreDetail, ":*") {
		t.Fatalf("expected the trailing * rule to match the base, got:\n%s", ignoreDetail)
	}

	gitOutput(t, project, "add", ".")
	staged := gitOutput(t, project, "ls-files", "--cached")
	if strings.Contains(staged, ".scbake-base/") {
		t.Fatalf("trailing hostile pattern did not hide the base; staged:\n%s", staged)
	}

	// Re-applying another template must not move an already-present block.
	out, err = runCLI("apply", "--with", "makefile", project)
	if err != nil {
		t.Fatalf("apply failed: %v\n%s", err, out)
	}
	if got := mustRead(t, gitignorePath); got != reordered {
		t.Fatalf("apply reordered the ignore block:\n got %q\nwant %q", got, reordered)
	}
}
