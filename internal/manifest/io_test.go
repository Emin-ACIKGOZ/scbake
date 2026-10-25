// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"sync"
	"testing"
	"time"
)

// writeManifestFile writes body as scbake.toml inside dir.
func writeManifestFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, fileutil.ManifestFileName), []byte(body), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}
}

// skipIfAncestorHasMarker keeps the marker-less regression fixtures meaningful
// when TMPDIR itself lives inside a project (for example TMPDIR=$PWD/.tmp).
// Such an environment cannot represent "no .git anywhere above"; skip with a
// clear reason instead of silently asserting the wrong policy. It checks the
// ancestors of fixtureRoot (not fixtureRoot itself), so the fixture may contain
// a hostile manifest below that root.
func skipIfAncestorHasMarker(tb testing.TB, fixtureRoot string) {
	tb.Helper()
	for dir := filepath.Dir(fixtureRoot); ; {
		for _, name := range []string{fileutil.ManifestFileName, fileutil.GitDir} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				tb.Skipf("fixture root %q is inside a project (%s); cannot build a marker-less fixture",
					fixtureRoot, filepath.Join(dir, name))
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

// TestFindProjectRoot_MarkerlessAncestorRejected is a regression test for
// SCB-017: a hostile manifest in an ancestor must not be adopted when no .git
// marker bounds the tree.
func TestFindProjectRoot_MarkerlessAncestorRejected(t *testing.T) {
	base := t.TempDir()
	skipIfAncestorHasMarker(t, base)

	evilDir := filepath.Join(base, "evil")
	victimDir := filepath.Join(evilDir, "victim", "nested")
	if err := os.MkdirAll(victimDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, evilDir, "[metadata]\nlicense = \"EVIL\"\n")

	found, err := FindProjectRoot(victimDir)
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}
	if found != victimDir {
		t.Errorf("hostile ancestor adopted. Want %s, Got %s", victimDir, found)
	}

	m, rootPath, err := Load(victimDir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if rootPath != victimDir {
		t.Errorf("Load root mismatch. Want %s, Got %s", victimDir, rootPath)
	}
	if len(m.Metadata) != 0 {
		t.Errorf("hostile metadata adopted: %v", m.Metadata)
	}
}

// TestFindProjectRoot_SymlinkedStartAncestorRejected documents that discovery
// is lexical: a symlink cannot widen ancestor trust because the walk uses the
// link path's parents, not the physical target's parents.
func TestFindProjectRoot_SymlinkedStartAncestorRejected(t *testing.T) {
	base := t.TempDir()
	skipIfAncestorHasMarker(t, base)

	sharedDir := filepath.Join(base, "shared")
	victimDir := filepath.Join(base, "real", "victim")
	if err := os.MkdirAll(sharedDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(victimDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, sharedDir, "[metadata]\nlicense = \"EVIL\"\n")

	link := filepath.Join(sharedDir, "link")
	if err := os.Symlink(victimDir, link); err != nil {
		t.Skipf("cannot create symlink on this platform: %v", err)
	}

	found, err := FindProjectRoot(link)
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}
	if found != link {
		t.Errorf("symlinked start adopted hostile ancestor. Want %s, Got %s", link, found)
	}

	m, rootPath, err := Load(link)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if rootPath != link {
		t.Errorf("Load root mismatch. Want %s, Got %s", link, rootPath)
	}
	if len(m.Metadata) != 0 {
		t.Errorf("hostile metadata adopted through symlink: %v", m.Metadata)
	}
}

// TestFindProjectRoot_RelativeAndDotDot pins the lexical normalization contract:
// relative start paths and dot segments resolve to an absolute, cleaned result.
func TestFindProjectRoot_RelativeAndDotDot(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	subDir := filepath.Join(repoDir, "sub", "dir")
	if err := os.MkdirAll(subDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, repoDir, "")
	if err := os.Mkdir(filepath.Join(repoDir, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	t.Chdir(repoDir)
	repoAbs, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		start string
	}{
		{start: "sub/dir"},
		{start: "./sub/./dir"},
		{start: "sub/../sub/dir"},
		{start: "sub/dir/.."},
	}
	for _, tt := range tests {
		t.Run(tt.start, func(t *testing.T) {
			found, err := FindProjectRoot(tt.start)
			if err != nil {
				t.Fatalf("FindProjectRoot failed: %v", err)
			}
			if !filepath.IsAbs(found) {
				t.Errorf("result is not absolute: %s", found)
			}
			if found != repoAbs {
				t.Errorf("path normalization mismatch. Want %s, Got %s", repoAbs, found)
			}
		})
	}
}

// TestFindProjectRoot_DeepTreeTerminates guards against unbounded walking or a
// per-level cost regression on a deep marker-less tree.
func TestFindProjectRoot_DeepTreeTerminates(t *testing.T) {
	base := t.TempDir()
	skipIfAncestorHasMarker(t, base)

	deep := base
	for i := 0; i < 48; i++ {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	for i := 0; i < 100; i++ {
		found, err := FindProjectRoot(deep)
		if err != nil {
			t.Fatalf("FindProjectRoot failed: %v", err)
		}
		if found != deep {
			t.Fatalf("deep tree fallback mismatch. Want %s, Got %s", deep, found)
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("100 deep-tree discoveries took %s, want < 2s", elapsed)
	}
}

// assertLoadsRootManifest checks that Load from start reads the manifest at
// rootDir with the expected content.
func assertLoadsRootManifest(t *testing.T, start, rootDir string) {
	t.Helper()
	m, rootPath, err := Load(start)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if rootPath != rootDir {
		t.Errorf("Load root mismatch. Want %s, Got %s", rootDir, rootPath)
	}
	if m.Metadata["license"] != "ROOT" {
		t.Errorf("root manifest content not loaded: %v", m.Metadata)
	}
}

func TestFindProjectRoot(t *testing.T) {
	// Setup:
	// /root/ (scbake.toml)
	// /root/src/cmd/

	baseDir := t.TempDir()
	rootDir := filepath.Join(baseDir, "root")
	srcDir := filepath.Join(rootDir, "src")
	cmdDir := filepath.Join(srcDir, "cmd")

	if err := os.MkdirAll(cmdDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	// Create manifest at root
	manifestPath := filepath.Join(rootDir, fileutil.ManifestFileName)
	if err := os.WriteFile(
		manifestPath,
		[]byte("[metadata]\nlicense = \"ROOT\"\n"),
		fileutil.PrivateFilePerms,
	); err != nil {
		t.Fatal(err)
	}
	// A .git marker at the repo root makes the boundary explicit; the manifest
	// still wins because it is the nearest marker.
	if err := os.Mkdir(filepath.Join(rootDir, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	// Test 1: Run from deep inside
	found, err := FindProjectRoot(cmdDir)
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}
	if found != rootDir {
		t.Errorf("Deep traversal failed. Want %s, Got %s", rootDir, found)
	}

	// Load from the same depth must read the root manifest, proving that the
	// co-located .git marker does not mask manifest discovery.
	assertLoadsRootManifest(t, cmdDir, rootDir)

	// Test 2: Run from root itself
	found, err = FindProjectRoot(rootDir)
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}
	if found != rootDir {
		t.Errorf("Root traversal failed. Want %s, Got %s", rootDir, found)
	}
}

func TestFindProjectRoot_NestedOverride(t *testing.T) {
	// Setup:
	// /repo/scbake.toml
	// /repo/sub/scbake.toml (Should override root)
	// /repo/sub/cmd/

	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, "repo")
	subDir := filepath.Join(repoDir, "sub")
	cmdDir := filepath.Join(subDir, "cmd")

	if err := os.MkdirAll(cmdDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	// Create BOTH manifests
	if err := os.WriteFile(filepath.Join(repoDir, fileutil.ManifestFileName), []byte("root=true"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, fileutil.ManifestFileName), []byte("sub=true"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	// Repository root marker: the documented monorepo case is Git-backed, and
	// the boundary makes the marker-less fallback irrelevant here.
	if err := os.Mkdir(filepath.Join(repoDir, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	// Run from /repo/sub/cmd -> Should stop at /repo/sub
	found, err := FindProjectRoot(cmdDir)
	if err != nil {
		t.Fatal(err)
	}
	if found != subDir {
		t.Errorf("Nested override failed. Want %s, Got %s", subDir, found)
	}

	// Run from /repo -> Should see /repo
	found, err = FindProjectRoot(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if found != repoDir {
		t.Errorf("Root lookup failed. Want %s, Got %s", repoDir, found)
	}
}

func TestFindProjectRoot_FromFilePath(t *testing.T) {
	// Setup: /tmp/root/scbake.toml and /tmp/root/main.go
	// User runs Load("/tmp/root/main.go")

	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	mainGo := filepath.Join(rootDir, "main.go")
	if err := os.WriteFile(mainGo, []byte("package main"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	found, err := FindProjectRoot(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	if found != rootDir {
		t.Errorf("File path input traversal failed. Want %s, Got %s", rootDir, found)
	}
}

func TestFindProjectRoot_GitFallback(t *testing.T) {
	baseDir := t.TempDir()
	gitRoot := filepath.Join(baseDir, "gitroot")
	childDir := filepath.Join(gitRoot, "child")

	if err := os.MkdirAll(childDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(gitRoot, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	found, err := FindProjectRoot(childDir)
	if err != nil {
		t.Fatal(err)
	}
	if found != gitRoot {
		t.Errorf("Git fallback failed. Want %s, Got %s", gitRoot, found)
	}
}

func TestFindProjectRoot_FallbackToStart(t *testing.T) {
	emptyDir := t.TempDir()

	found, err := FindProjectRoot(emptyDir)
	if err != nil {
		t.Fatal(err)
	}
	// Fallback should normalize to the directory itself
	if found != emptyDir {
		t.Errorf("Fallback failed. Want %s, Got %s", emptyDir, found)
	}
}

func TestLoad_EmptyManifestDefaultsVersion(t *testing.T) {
	rootDir := t.TempDir()

	// Simulate `scbake new`: an existing but empty scbake.toml.
	if err := os.WriteFile(filepath.Join(rootDir, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	m, _, err := Load(rootDir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if m.SbakeVersion == "" {
		t.Fatal("expected non-empty SbakeVersion for an empty manifest")
	}
	if m.SbakeVersion != DefaultSbakeVersion {
		t.Errorf("SbakeVersion mismatch. Want %q, Got %q", DefaultSbakeVersion, m.SbakeVersion)
	}
}

func TestLoad_AbsentManifestDefaultsVersion(t *testing.T) {
	rootDir := t.TempDir()

	// No scbake.toml present: fresh apply path.
	m, _, err := Load(rootDir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if m.SbakeVersion != DefaultSbakeVersion {
		t.Errorf("SbakeVersion mismatch. Want %q, Got %q", DefaultSbakeVersion, m.SbakeVersion)
	}
}

func TestLoadAndSave(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Test Load on empty dir (should default)
	m, root, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if len(m.Projects) != 0 {
		t.Error("expected empty projects")
	}
	if root != tmpDir {
		t.Errorf("Root mismatch. Want %s, Got %s", tmpDir, root)
	}

	// 2. Test Save (Atomic)
	m.Projects = append(m.Projects, types.Project{Name: "test"})
	if err := Save(m, tmpDir); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// 3. Verify file exists
	mfPath := filepath.Join(tmpDir, fileutil.ManifestFileName)
	if _, err := os.Stat(mfPath); err != nil {
		t.Error("Manifest file was not created")
	}

	// Verify no temp file is left behind.
	leftovers, err := filepath.Glob(filepath.Join(tmpDir, fileutil.ManifestFileName+".*.tmp"))
	if err != nil {
		t.Fatalf("glob temp manifest files failed: %v", err)
	}
	if len(leftovers) != 0 {
		t.Errorf("temp manifest file was not cleaned up: %v", leftovers)
	}

	// 4. Reload
	m2, _, err := Load(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.Projects) != 1 {
		t.Error("Persistence failed")
	}
}

// TestSave_DoesNotReuseExistingTempPath verifies Save writes through a random
// temp name and leaves a file at the old fixed "scbake.toml.tmp" path untouched.
func TestSave_DoesNotReuseExistingTempPath(t *testing.T) {
	rootDir := t.TempDir()

	// Create a file at the old fixed temp path; Save must leave it untouched.
	existingFile := filepath.Join(rootDir, fileutil.ManifestFileName+".tmp")
	if err := os.WriteFile(existingFile, []byte("existing"), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	m := &types.Manifest{SbakeVersion: DefaultSbakeVersion}
	if err := Save(m, rootDir); err != nil {
		t.Fatalf("Save() failed with an existing temp path: %v", err)
	}

	got, err := os.ReadFile(existingFile) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("Save() removed the existing file: %v", err)
	}
	if string(got) != "existing" {
		t.Errorf("Save() overwrote the existing file: got %q", got)
	}
	leftovers, err := filepath.Glob(filepath.Join(rootDir, fileutil.ManifestFileName+".*.tmp"))
	if err != nil {
		t.Fatalf("glob temp manifest files failed: %v", err)
	}
	if len(leftovers) != 0 {
		t.Errorf("Save() left temp files behind: %v", leftovers)
	}
}

// markerlessMonorepoFixture builds a non-Git monorepo:
//
//	repo/scbake.toml
//	repo/sub/scbake.toml
//	repo/sub/cmd/          (no markers)
//
// It returns the repo, sub, and cmd directories.
func markerlessMonorepoFixture(t *testing.T) (repoDir, subDir, cmdDir string) {
	t.Helper()
	base := t.TempDir()
	skipIfAncestorHasMarker(t, base)

	repoDir = filepath.Join(base, "repo")
	subDir = filepath.Join(repoDir, "sub")
	cmdDir = filepath.Join(subDir, "cmd")
	if err := os.MkdirAll(cmdDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, repoDir, "")
	writeManifestFile(t, subDir, "")
	return repoDir, subDir, cmdDir
}

// TestFindProjectRoot_MarkerlessAncestorDefaultRejected pins the safe default:
// a marker-less tree falls back to the start directory.
func TestFindProjectRoot_MarkerlessAncestorDefaultRejected(t *testing.T) {
	_, _, cmdDir := markerlessMonorepoFixture(t)

	found, err := FindProjectRoot(cmdDir)
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}
	if found != cmdDir {
		t.Errorf("default boundary mismatch. Want %s, Got %s", cmdDir, found)
	}
}

// TestFindProjectRoot_MarkerlessAncestorOptIn preserves the documented non-Git
// monorepo workflow behind the explicit opt-in.
func TestFindProjectRoot_MarkerlessAncestorOptIn(t *testing.T) {
	_, subDir, cmdDir := markerlessMonorepoFixture(t)

	opts := DiscoveryOptions{AllowAncestorSearch: true}
	found, err := FindProjectRootWithOptions(cmdDir, opts)
	if err != nil {
		t.Fatalf("FindProjectRootWithOptions failed: %v", err)
	}
	if found != subDir {
		t.Errorf("opt-in ancestor search mismatch. Want %s, Got %s", subDir, found)
	}

	m, rootPath, err := LoadWithOptions(cmdDir, opts)
	if err != nil {
		t.Fatalf("LoadWithOptions failed: %v", err)
	}
	if rootPath != subDir {
		t.Errorf("LoadWithOptions root mismatch. Want %s, Got %s", subDir, rootPath)
	}
	if m == nil {
		t.Fatal("LoadWithOptions returned a nil manifest")
	}
}

// TestFindProjectRoot_MarkerlessLocalManifestWins: a manifest in the start
// directory is always local, even with ancestor search enabled.
func TestFindProjectRoot_MarkerlessLocalManifestWins(t *testing.T) {
	repoDir, _, _ := markerlessMonorepoFixture(t)

	opts := DiscoveryOptions{AllowAncestorSearch: true}
	found, err := FindProjectRootWithOptions(repoDir, opts)
	if err != nil {
		t.Fatalf("FindProjectRootWithOptions failed: %v", err)
	}
	if found != repoDir {
		t.Errorf("local manifest should win. Want %s, Got %s", repoDir, found)
	}
}

// TestFindProjectRoot_ManifestAboveGitRootRejected proves the opt-in never
// crosses a Git root: a manifest above the repository boundary is ignored.
func TestFindProjectRoot_ManifestAboveGitRootRejected(t *testing.T) {
	base := t.TempDir()
	outerDir := filepath.Join(base, "outer")
	repoDir := filepath.Join(outerDir, "repo")
	cmdDir := filepath.Join(repoDir, "pkg", "cmd")
	if err := os.MkdirAll(cmdDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, outerDir, "[metadata]\nlicense = \"EVIL\"\n")

	for _, opts := range []DiscoveryOptions{{}, {AllowAncestorSearch: true}} {
		found, err := FindProjectRootWithOptions(cmdDir, opts)
		if err != nil {
			t.Fatalf("FindProjectRootWithOptions(%+v) failed: %v", opts, err)
		}
		if found != repoDir {
			t.Errorf("FindProjectRootWithOptions(%+v) = %s, want %s", opts, found, repoDir)
		}

		m, rootPath, err := LoadWithOptions(cmdDir, opts)
		if err != nil {
			t.Fatalf("LoadWithOptions(%+v) failed: %v", opts, err)
		}
		if rootPath != repoDir {
			t.Errorf("LoadWithOptions(%+v) root = %s, want %s", opts, rootPath, repoDir)
		}
		if len(m.Metadata) != 0 {
			t.Errorf("LoadWithOptions(%+v) adopted metadata above the Git root: %v", opts, m.Metadata)
		}
	}
}

// TestFindProjectRoot_GitMarkerFile covers worktree-style .git files, which
// os.Stat recognizes like a directory.
func TestFindProjectRoot_GitMarkerFile(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	cmdDir := filepath.Join(repoDir, "pkg", "cmd")
	if err := os.MkdirAll(cmdDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, fileutil.GitDir), []byte("gitdir: ../.git/worktrees/x\n"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	for _, opts := range []DiscoveryOptions{{}, {AllowAncestorSearch: true}} {
		found, err := FindProjectRootWithOptions(cmdDir, opts)
		if err != nil {
			t.Fatalf("FindProjectRootWithOptions(%+v) failed: %v", opts, err)
		}
		if found != repoDir {
			t.Errorf("FindProjectRootWithOptions(%+v) = %s, want %s", opts, found, repoDir)
		}
	}
}

// TestFindProjectRoot_NestedGitRootStopsWalk proves the nearest Git marker is
// the boundary, even when an outer repository has a manifest.
func TestFindProjectRoot_NestedGitRootStopsWalk(t *testing.T) {
	base := t.TempDir()
	outerDir := filepath.Join(base, "outer")
	innerDir := filepath.Join(outerDir, "inner")
	pkgDir := filepath.Join(innerDir, "pkg")
	if err := os.MkdirAll(pkgDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outerDir, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(innerDir, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, outerDir, "[metadata]\nlicense = \"EVIL\"\n")

	for _, opts := range []DiscoveryOptions{{}, {AllowAncestorSearch: true}} {
		found, err := FindProjectRootWithOptions(pkgDir, opts)
		if err != nil {
			t.Fatalf("FindProjectRootWithOptions(%+v) failed: %v", opts, err)
		}
		if found != innerDir {
			t.Errorf("FindProjectRootWithOptions(%+v) = %s, want %s", opts, found, innerDir)
		}
	}
}

// TestLoadWithOptions_Concurrent checks that discovery options are per-call
// state: concurrent default and opt-in loads must not influence each other.
func TestLoadWithOptions_Concurrent(t *testing.T) {
	base := t.TempDir()
	monoDir := filepath.Join(base, "mono")
	subDir := filepath.Join(monoDir, "sub")
	if err := os.MkdirAll(subDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, monoDir, "[metadata]\nlicense = \"MONO\"\n")

	const (
		workers    = 16
		iterations = 25
	)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			opts := DiscoveryOptions{AllowAncestorSearch: w%2 == 1}
			for i := 0; i < iterations; i++ {
				m, rootPath, err := LoadWithOptions(subDir, opts)
				if err != nil {
					t.Errorf("worker %d: LoadWithOptions failed: %v", w, err)
					return
				}
				if opts.AllowAncestorSearch {
					if rootPath != monoDir {
						t.Errorf("worker %d: opt-in root = %s, want %s", w, rootPath, monoDir)
						return
					}
					if m.Metadata["license"] != "MONO" {
						t.Errorf("worker %d: opt-in metadata = %v, want license=MONO", w, m.Metadata)
						return
					}
					continue
				}
				if rootPath != subDir {
					t.Errorf("worker %d: default root = %s, want %s", w, rootPath, subDir)
					return
				}
				if len(m.Metadata) != 0 {
					t.Errorf("worker %d: default load adopted ancestor metadata: %v", w, m.Metadata)
					return
				}
			}
		}(w)
	}
	wg.Wait()
}

// BenchmarkFindProjectRoot_MarkerlessDeepTree measures the new worst case: a
// marker-less candidate forces a full walk to the filesystem root.
func BenchmarkFindProjectRoot_MarkerlessDeepTree(b *testing.B) {
	base := b.TempDir()
	skipIfAncestorHasMarker(b, base)

	if err := os.WriteFile(filepath.Join(base, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms); err != nil {
		b.Fatal(err)
	}
	deep := base
	for i := 0; i < 48; i++ {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, fileutil.DirPerms); err != nil {
		b.Fatal(err)
	}

	if found, err := FindProjectRoot(deep); err != nil {
		b.Fatalf("FindProjectRoot failed: %v", err)
	} else if found != deep {
		b.Fatalf("FindProjectRoot = %s, want %s", found, deep)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := FindProjectRoot(deep); err != nil {
			b.Fatalf("FindProjectRoot failed: %v", err)
		}
	}
}
