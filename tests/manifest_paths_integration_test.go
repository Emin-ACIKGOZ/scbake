// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"scbake/internal/types"
)

// This file is the independent regression matrix for issue #134: every accepted
// spelling of the same target must record one clean, manifest-root-relative
// path for both [[projects]] and [[templates]], and generated Makefile/CI
// consumers must cd to that path. It also pins the deliberate migration limits:
// absolute legacy paths are repaired on the next mutating run, while relative
// legacy spellings stay byte-identical and must not cause a wrong-directory
// write.

const manifestPathTemplates = "makefile,editorconfig,ci_github"

// runManifestCLI runs the compiled scbake binary in dir. It is a local helper so
// this file can be copied into a pristine base tree for the fail-on-base proof.
func runManifestCLI(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binaryPath, args...) //nolint:gosec // test-controlled binary and arguments
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PWD="+dir,
		"GIT_AUTHOR_NAME=Test Runner",
		"GIT_AUTHOR_EMAIL=runner@test.com",
		"GIT_COMMITTER_NAME=Test Runner",
		"GIT_COMMITTER_EMAIL=runner@test.com",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// manifestPathRepo creates work/repo/svc. Unless ownGit is set, the git marker
// sits at work/repo, so work/repo is the manifest root; with ownGit the marker
// sits in work/repo/svc, making the target its own root.
func manifestPathRepo(t *testing.T, ownGit bool) (work, repo string) {
	t.Helper()
	work = t.TempDir()
	repo = filepath.Join(work, "repo")
	mustMkdirAll(t, filepath.Join(repo, "svc"))
	if ownGit {
		mustMkdirAll(t, filepath.Join(repo, "svc", ".git"))
	} else {
		mustMkdirAll(t, filepath.Join(repo, ".git"))
	}
	return work, repo
}

// manifestSignature is the canonical projection compared across spellings. It
// includes every entry and ledger key, so a duplicate or a differing path/hash
// changes the signature.
func manifestSignature(m types.Manifest) string {
	lines := make([]string, 0, len(m.Projects)+len(m.Templates)+len(m.ManagedFiles))
	for _, p := range m.Projects {
		lines = append(lines, "project:"+p.Name+":"+p.Language+":"+p.Path)
	}
	for _, tm := range m.Templates {
		lines = append(lines, "template:"+tm.Name+":"+tm.Path)
	}
	for key, hash := range m.ManagedFiles {
		lines = append(lines, "managed:"+key+"="+hash)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func requireProjectCount(t *testing.T, m types.Manifest, want int) {
	t.Helper()
	if len(m.Projects) != want {
		t.Fatalf("manifest has %d projects, want %d: %+v", len(m.Projects), want, m.Projects)
	}
}

func requireSingleProject(t *testing.T, m types.Manifest, path, name, language string) {
	t.Helper()
	requireProjectCount(t, m, 1)
	p := m.Projects[0]
	if p.Path != path || p.Name != name || p.Language != language {
		t.Errorf("project = {name:%q path:%q language:%q}, want {name:%q path:%q language:%q}",
			p.Name, p.Path, p.Language, name, path, language)
	}
}

func requireTemplatesAt(t *testing.T, m types.Manifest, path string, names ...string) {
	t.Helper()
	if len(m.Templates) != len(names) {
		t.Fatalf("manifest has %d templates, want %d: %+v", len(m.Templates), len(names), m.Templates)
	}
	got := make(map[string]string, len(m.Templates))
	for _, tm := range m.Templates {
		got[tm.Name] = tm.Path
	}
	for _, name := range names {
		if got[name] != path {
			t.Errorf("template %q path = %q, want %q", name, got[name], path)
		}
	}
}

func requireNoAbsolutePaths(t *testing.T, m types.Manifest) {
	t.Helper()
	for _, p := range m.Projects {
		if filepath.IsAbs(p.Path) {
			t.Errorf("project %q records an absolute path %q", p.Name, p.Path)
		}
	}
	for _, tm := range m.Templates {
		if filepath.IsAbs(tm.Path) {
			t.Errorf("template %q records an absolute path %q", tm.Name, tm.Path)
		}
	}
}

func requireNoArtifactsIn(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("unexpected artifact %s in %s (stat err: %v)", name, dir, err)
		}
	}
}

func requireNoScbakeNewFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".scbake-new") {
			t.Errorf("stale artifact left behind: %s", path)
		}
		if entry.IsDir() && (entry.Name() == fileutilInternalDir || strings.HasPrefix(entry.Name(), "stage-")) {
			t.Errorf("transaction residue left behind: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

// fileutilInternalDir mirrors the private state directory name without importing
// internal util code that a base tree may not expose to this test.
const fileutilInternalDir = ".scbake"

// requireConsumers checks the generated Makefile and GitHub workflow cd to rel
// from the manifest root, never to the root, a traversal, or an absolute path.
func requireConsumers(t *testing.T, manifestRoot, rel, repoAbs string) {
	t.Helper()
	targetDir := filepath.Join(manifestRoot, filepath.FromSlash(rel))

	makefile := mustReadFile(t, filepath.Join(targetDir, "Makefile"))
	if !strings.Contains(makefile, "(cd "+rel+" &&") {
		t.Errorf("Makefile does not cd to %q:\n%s", rel, makefile)
	}
	if rel != "." {
		for _, bad := range []string{"(cd ./", "(cd ../", "(cd . "} {
			if strings.Contains(makefile, bad) {
				t.Errorf("Makefile contains wrong cd target %q:\n%s", bad, makefile)
			}
		}
	}
	if strings.Contains(makefile, repoAbs) {
		t.Errorf("Makefile contains an absolute path %q", repoAbs)
	}

	workflow := mustReadFile(t, filepath.Join(targetDir, ".github", "workflows", "main.yml"))
	found := false
	for _, line := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "cd "+rel {
			found = true
		}
		if rel != "." && (strings.HasPrefix(trimmed, "cd .") || strings.HasPrefix(trimmed, "cd ..")) {
			t.Errorf("workflow contains wrong cd line %q", trimmed)
		}
	}
	if !found {
		t.Errorf("workflow does not contain `cd %s`:\n%s", rel, workflow)
	}
	if strings.Contains(workflow, repoAbs) {
		t.Errorf("workflow contains an absolute path %q", repoAbs)
	}
}

type manifestSnapshot struct {
	data []byte
	info os.FileInfo
}

func snapshotManifest(t *testing.T, path string) manifestSnapshot {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return manifestSnapshot{data: data, info: info}
}

func requireManifestUnchanged(t *testing.T, path string, before manifestSnapshot) {
	t.Helper()
	after := snapshotManifest(t, path)
	if !bytes.Equal(before.data, after.data) {
		t.Errorf("%s bytes changed:\nbefore:\n%s\nafter:\n%s", path, before.data, after.data)
	}
	if !os.SameFile(before.info, after.info) {
		t.Errorf("%s was replaced (inode changed)", path)
	}
	if !before.info.ModTime().Equal(after.info.ModTime()) {
		t.Errorf("%s mtime changed: %v -> %v", path, before.info.ModTime(), after.info.ModTime())
	}
}

// TestManifestRecordsRootRelativePaths is the core D4 matrix. Each subtest uses
// a fresh fixture and asserts the recorded paths, the generated consumers, and
// that update selects the same directory from any spelling.
//
// It is also the end-to-end update-direction regression: when apply is invoked
// from the target with ".", master records "." and can no longer update the
// directory it wrote to. The matrix covers the whole failure surface of that
// bug — the root update must refuse and write nothing, updates at the target
// and from every alternate spelling must succeed idempotently, and the recorded
// entries must stay root-relative and unduplicated.
func TestManifestRecordsRootRelativePaths(t *testing.T) {
	spellings := []struct {
		name   string
		ownGit bool
		cwd    func(work, repo string) string
		arg    func(work, repo string) string
	}{
		{"root-relative", false,
			func(_, repo string) string { return repo },
			func(_, _ string) string { return "svc" }},
		{"root-dot", false,
			func(_, repo string) string { return repo },
			func(_, _ string) string { return "./svc" }},
		{"root-traversal", false,
			func(_, repo string) string { return repo },
			func(_, _ string) string { return "svc/../svc" }},
		{"root-trailing", false,
			func(_, repo string) string { return repo },
			func(_, _ string) string { return "svc/" }},
		{"subdir-dot", false,
			func(_, repo string) string { return filepath.Join(repo, "svc") },
			func(_, _ string) string { return "." }},
		{"subdir-parent-spelling", false,
			func(_, repo string) string { return filepath.Join(repo, "svc") },
			func(_, _ string) string { return "../svc" }},
		{"parent-relative", false,
			func(work, _ string) string { return work },
			func(_, _ string) string { return "repo/svc" }},
		{"absolute", false,
			func(_, repo string) string { return repo },
			func(_, repo string) string { return filepath.Join(repo, "svc") }},
		{"target-own-root", true,
			func(work, _ string) string { return work },
			func(_, _ string) string { return "repo/svc" }},
	}

	signatures := make(map[string]string, len(spellings))
	for _, tc := range spellings {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			work, repo := manifestPathRepo(t, tc.ownGit)
			manifestRoot := repo
			wantPath := "svc"
			if tc.ownGit {
				manifestRoot = filepath.Join(repo, "svc")
				wantPath = "."
			}

			output, err := runManifestCLI(t, tc.cwd(work, repo),
				"apply", "--lang", "go", "--with", manifestPathTemplates, tc.arg(work, repo))
			if err != nil {
				t.Fatalf("apply failed: %v\n%s", err, output)
			}

			m := decodeManifest(t, manifestRoot)
			requireSingleProject(t, m, wantPath, "svc", "go")
			requireTemplatesAt(t, m, wantPath, "makefile", "editorconfig", "ci_github")
			requireNoAbsolutePaths(t, m)
			if !tc.ownGit {
				// The own-root case has a different manifest root (svc rather
				// than repo), so its ledger keys and path-sensitive template
				// content legitimately differ.
				signatures[tc.name] = manifestSignature(m)
			}

			if wantPath == "svc" {
				for key := range m.ManagedFiles {
					if !strings.HasPrefix(key, "svc/") {
						t.Errorf("managed key %q is not under svc/", key)
					}
				}
			} else {
				for key := range m.ManagedFiles {
					if strings.HasPrefix(key, "./") || strings.HasPrefix(key, "../") {
						t.Errorf("managed key %q is not clean root-relative", key)
					}
				}
			}

			requireConsumers(t, manifestRoot, wantPath, repo)
			requireNoScbakeNewFiles(t, work)

			if wantPath == "svc" {
				requireNoArtifactsIn(t, repo, "main.go", "Makefile", ".editorconfig", "go.mod", ".github", ".scbake")
				baseEntries, err := os.ReadDir(filepath.Join(repo, ".scbake-base"))
				if err != nil {
					t.Fatalf("reading committed base: %v", err)
				}
				if len(baseEntries) != 1 || baseEntries[0].Name() != "svc" {
					t.Errorf("committed base entries = %v, want only svc", baseEntries)
				}
			} else {
				requireNoArtifactsIn(t, repo, "scbake.toml", "main.go", "Makefile", ".editorconfig", "go.mod", ".github", ".scbake", ".scbake-base")
			}

			manifestPath := filepath.Join(manifestRoot, "scbake.toml")
			before := snapshotManifest(t, manifestPath)

			// Root update over a subproject-only manifest must fail closed and
			// write nothing. A target whose own directory is the root has no
			// separate root invocation to test.
			if wantPath == "svc" {
				out, err := runManifestCLI(t, manifestRoot, "update")
				if err == nil {
					t.Fatalf("root update over a subproject-only manifest succeeded:\n%s", out)
				}
				if !strings.Contains(out, "records no templates") {
					t.Errorf("root update error does not explain the missing selection:\n%s", out)
				}
				requireManifestUnchanged(t, manifestPath, before)
				requireNoArtifactsIn(t, repo, "main.go", "Makefile", ".editorconfig", "go.mod", ".github", ".scbake")
			}

			// update in the target succeeds and is idempotent, including a
			// repeated bare update (no explicit target argument).
			targetCwd := filepath.Join(manifestRoot, filepath.FromSlash(wantPath))
			sigBefore := manifestSignature(decodeManifest(t, manifestRoot))
			for i := 0; i < 2; i++ {
				out, err := runManifestCLI(t, targetCwd, "update")
				if err != nil {
					t.Fatalf("update #%d in the applied directory failed: %v\n%s", i+1, err, out)
				}
				sigAfter := manifestSignature(decodeManifest(t, manifestRoot))
				if sigAfter != sigBefore {
					t.Errorf("update #%d changed the manifest signature:\nbefore:\n%s\nafter:\n%s", i+1, sigBefore, sigAfter)
				}
			}

			// Alternate spellings of the same target must select the same
			// entries and never duplicate or rewrite them.
			if wantPath == "svc" && !tc.ownGit {
				alternates := []struct {
					cwd string
					arg string
				}{
					{manifestRoot, "./svc"},
					{manifestRoot, "svc/"},
					{filepath.Dir(manifestRoot), filepath.Base(manifestRoot) + "/svc"},
				}
				for _, alt := range alternates {
					out, err := runManifestCLI(t, alt.cwd, "update", alt.arg)
					if err != nil {
						t.Fatalf("update %q from %s failed: %v\n%s", alt.arg, alt.cwd, err, out)
					}
					if got := manifestSignature(decodeManifest(t, manifestRoot)); got != sigBefore {
						t.Errorf("update %q changed the manifest signature:\nbefore:\n%s\nafter:\n%s", alt.arg, sigBefore, got)
					}
				}
			}

			requireNoScbakeNewFiles(t, work)
		})
	}

	t.Run("all-spellings-share-one-signature", func(t *testing.T) {
		wantCount := len(spellings) - 1 // target-own-root has a different manifest root
		if len(signatures) != wantCount {
			t.Fatalf("collected %d signatures, want %d", len(signatures), wantCount)
		}
		want := signatures["root-relative"]
		for _, tc := range spellings {
			if tc.ownGit {
				continue
			}
			if got := signatures[tc.name]; got != want {
				t.Errorf("signature for %q differs from root-relative:\nwant:\n%s\ngot:\n%s", tc.name, want, got)
			}
		}
	})
}

// TestManifestRootProjectKeepsDot covers an apply at the manifest root itself:
// both entry types record "." and update at the root remains valid.
func TestManifestRootProjectKeepsDot(t *testing.T) {
	_, repo := manifestPathRepo(t, false)

	output, err := runManifestCLI(t, repo, "apply", "--lang", "go", "--with", manifestPathTemplates, ".")
	if err != nil {
		t.Fatalf("apply . at the root failed: %v\n%s", err, output)
	}

	m := decodeManifest(t, repo)
	requireSingleProject(t, m, ".", "repo", "go")
	requireTemplatesAt(t, m, ".", "makefile", "editorconfig", "ci_github")
	requireConsumers(t, repo, ".", repo)

	before := manifestSignature(m)
	out, err := runManifestCLI(t, repo, "update")
	if err != nil {
		t.Fatalf("update at the root failed: %v\n%s", err, out)
	}
	if got := manifestSignature(decodeManifest(t, repo)); got != before {
		t.Errorf("root update changed the manifest signature:\nbefore:\n%s\nafter:\n%s", before, got)
	}
	requireNoScbakeNewFiles(t, repo)
}

// TestTemplateOnlyApplyFromSubdirectory covers a template-only apply from the
// target: the template path is the root-relative subproject, there is no
// project entry, and update in the target works.
func TestTemplateOnlyApplyFromSubdirectory(t *testing.T) {
	_, repo := manifestPathRepo(t, false)
	target := filepath.Join(repo, "svc")

	output, err := runManifestCLI(t, target, "apply", "--with", "editorconfig", ".")
	if err != nil {
		t.Fatalf("template-only apply failed: %v\n%s", err, output)
	}

	m := decodeManifest(t, repo)
	requireProjectCount(t, m, 0)
	requireTemplatesAt(t, m, "svc", "editorconfig")
	requireNoArtifactsIn(t, repo, ".editorconfig", "Makefile", "main.go", ".scbake")

	out, err := runManifestCLI(t, target, "update")
	if err != nil {
		t.Fatalf("update in the template-only target failed: %v\n%s", err, out)
	}
	if got := manifestSignature(decodeManifest(t, repo)); got != manifestSignature(m) {
		t.Errorf("update changed the template-only manifest")
	}
	requireNoScbakeNewFiles(t, repo)
}

// TestApplyDryRunWritesNothing pins dry-run parity for a subdirectory apply.
func TestApplyDryRunWritesNothing(t *testing.T) {
	_, repo := manifestPathRepo(t, false)
	target := filepath.Join(repo, "svc")

	output, err := runManifestCLI(t, target, "apply", "--dry-run", "--with", "editorconfig", ".")
	if err != nil {
		t.Fatalf("dry-run apply failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(repo, "scbake.toml")); !os.IsNotExist(err) {
		t.Errorf("dry-run apply created a manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".editorconfig")); !os.IsNotExist(err) {
		t.Errorf("dry-run apply created a template file: %v", err)
	}
	requireNoScbakeNewFiles(t, repo)
}

// legacyAbsoluteManifestFixture applies a normal subproject and then rewrites
// every recorded path to the equivalent absolute path, reproducing a manifest
// written by an older build.
func legacyAbsoluteManifestFixture(t *testing.T) (repo, manifestPath, absTarget string) {
	t.Helper()
	_, repo = manifestPathRepo(t, false)
	absTarget = filepath.Join(repo, "svc")
	out, err := runManifestCLI(t, repo, "apply", "--lang", "go", "--with", "editorconfig", "svc")
	if err != nil {
		t.Fatalf("apply failed: %v\n%s", err, out)
	}
	manifestPath = filepath.Join(repo, "scbake.toml")
	data := mustReadFile(t, manifestPath)
	if !strings.Contains(data, `path = "svc"`) {
		t.Fatalf("fixture manifest does not record the canonical path:\n%s", data)
	}
	data = strings.ReplaceAll(data, `path = "svc"`, fmt.Sprintf("path = %q", absTarget))
	if !strings.Contains(data, absTarget) {
		t.Fatalf("fixture failed to absolutise paths:\n%s", data)
	}
	mustWriteFile(t, manifestPath, data)
	return repo, manifestPath, absTarget
}

func requireRepairedManifest(t *testing.T, manifestPath, wantPath, legacyAbsolute string) {
	t.Helper()
	raw := mustReadFile(t, manifestPath)
	m := decodeManifest(t, filepath.Dir(manifestPath))
	requireSingleProject(t, m, wantPath, "svc", "go")
	requireTemplatesAt(t, m, wantPath, "editorconfig")
	requireNoAbsolutePaths(t, m)
	if strings.Contains(raw, legacyAbsolute) {
		t.Errorf("manifest still records the absolute legacy path %q:\n%s", legacyAbsolute, raw)
	}
}

// TestLegacyAbsoluteManifestRepaired proves that absolute legacy entries are
// rebased in memory on load and persisted by the next mutating run, including
// project-only manifests that the validator would otherwise reject.
func TestLegacyAbsoluteManifestRepaired(t *testing.T) {
	t.Run("update svc", func(t *testing.T) {
		repo, manifestPath, legacyAbsolute := legacyAbsoluteManifestFixture(t)
		out, err := runManifestCLI(t, repo, "update", "svc")
		if err != nil {
			t.Fatalf("update over an absolute legacy manifest failed: %v\n%s", err, out)
		}
		requireRepairedManifest(t, manifestPath, "svc", legacyAbsolute)
	})

	t.Run("drift --repair svc", func(t *testing.T) {
		repo, manifestPath, legacyAbsolute := legacyAbsoluteManifestFixture(t)
		out, err := runManifestCLI(t, repo, "drift", "--repair", "svc")
		if err != nil {
			t.Fatalf("drift --repair over an absolute legacy manifest failed: %v\n%s", err, out)
		}
		requireRepairedManifest(t, manifestPath, "svc", legacyAbsolute)
	})

	t.Run("project-only absolute is accepted by apply", func(t *testing.T) {
		_, repo := manifestPathRepo(t, false)
		absTarget := filepath.Join(repo, "svc")
		manifestPath := filepath.Join(repo, "scbake.toml")
		mustWriteFile(t, manifestPath, fmt.Sprintf(`scbake_version = "v0.3.0"

[[projects]]
  name = "svc"
  path = %q
  language = "go"
`, absTarget))

		out, err := runManifestCLI(t, repo, "apply", "--with", "editorconfig", "svc")
		if err != nil {
			t.Fatalf("apply over a project-only absolute legacy manifest failed: %v\n%s", err, out)
		}
		m := decodeManifest(t, repo)
		requireSingleProject(t, m, "svc", "svc", "go")
		requireTemplatesAt(t, m, "svc", "editorconfig")
		if raw := mustReadFile(t, manifestPath); strings.Contains(raw, `path = "/`) {
			t.Errorf("manifest still records an absolute path:\n%s", raw)
		}
	})
}

// TestLegacyRelativeManifestStaysInert pins the E8 counterexample: a relative
// legacy entry is ambiguous, so cleaning it would make an inert entry match a
// different directory. It must stay byte-identical and must not cause a write.
func TestLegacyRelativeManifestStaysInert(t *testing.T) {
	_, repo := manifestPathRepo(t, false)
	manifestPath := filepath.Join(repo, "scbake.toml")
	const legacy = `scbake_version = "v0.3.0"

[[templates]]
  name = "editorconfig"
  path = "./nested"

[managed_files]
  "svc/nested/.editorconfig" = "0000000000000000000000000000000000000000000000000000000000000000"
`
	mustWriteFile(t, manifestPath, legacy)
	mustWriteFile(t, filepath.Join(repo, "svc", "nested", ".editorconfig"), "user content\n")
	before := snapshotManifest(t, manifestPath)

	out, err := runManifestCLI(t, repo, "update", "nested")
	if err == nil {
		t.Fatalf("update accepted the ambiguous relative legacy entry:\n%s", out)
	}
	if !strings.Contains(out, "records no templates") {
		t.Errorf("unexpected error for the inert legacy entry:\n%s", out)
	}
	requireManifestUnchanged(t, manifestPath, before)
	if _, err := os.Stat(filepath.Join(repo, "nested", ".editorconfig")); !os.IsNotExist(err) {
		t.Errorf("update wrote a wrong-directory file repo/nested/.editorconfig (stat err: %v)", err)
	}
	if got := mustReadFile(t, manifestPath); got != legacy {
		t.Errorf("inert legacy manifest was rewritten:\n%s", got)
	}
	requireNoScbakeNewFiles(t, repo)
}

// TestReadOnlyCommandsDoNotRewriteManifest proves load-time rebasing is in
// memory only: list, drift, and dry runs leave bytes, inode, and mtime alone.
// The update dry-run is allowed to fail selection on a legacy absolute manifest
// (on master it does); the no-write property is what this test pins.
func TestReadOnlyCommandsDoNotRewriteManifest(t *testing.T) {
	repo, manifestPath, _ := legacyAbsoluteManifestFixture(t)
	before := snapshotManifest(t, manifestPath)

	commands := []struct {
		name        string
		args        []string
		mustSucceed bool
	}{
		{"list projects", []string{"list", "projects"}, true},
		{"drift", []string{"drift"}, true},
		{"update --dry-run svc", []string{"update", "--dry-run", "svc"}, false},
		{"apply --dry-run svc", []string{"apply", "--dry-run", "--with", "editorconfig", "svc"}, false},
	}
	for _, cmd := range commands {
		out, err := runManifestCLI(t, repo, cmd.args...)
		if err != nil && cmd.mustSucceed {
			t.Fatalf("%s failed: %v\n%s", cmd.name, err, out)
		}
		if err != nil {
			t.Logf("%s did not select a legacy absolute entry (expected before repair): %v\n%s", cmd.name, err, out)
		}
		requireManifestUnchanged(t, manifestPath, before)
	}
	requireNoScbakeNewFiles(t, repo)

	// A canonical manifest must support the same dry run.
	_, canonicalRepo := manifestPathRepo(t, false)
	if out, err := runManifestCLI(t, canonicalRepo, "apply", "--lang", "go", "--with", "editorconfig", "svc"); err != nil {
		t.Fatalf("canonical fixture apply failed: %v\n%s", err, out)
	}
	canonicalManifest := filepath.Join(canonicalRepo, "scbake.toml")
	canonicalBefore := snapshotManifest(t, canonicalManifest)
	if out, err := runManifestCLI(t, canonicalRepo, "update", "--dry-run", "svc"); err != nil {
		t.Fatalf("update --dry-run on a canonical manifest failed: %v\n%s", err, out)
	}
	requireManifestUnchanged(t, canonicalManifest, canonicalBefore)
	if out, err := runManifestCLI(t, canonicalRepo, "apply", "--dry-run", "--with", "editorconfig", "svc"); err != nil {
		t.Fatalf("apply --dry-run on a canonical manifest failed: %v\n%s", err, out)
	}
	requireManifestUnchanged(t, canonicalManifest, canonicalBefore)
}
