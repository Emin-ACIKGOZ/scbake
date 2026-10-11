// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goInstallRe matches a `go install <module>@<version>` invocation and captures
// the module path and the version selector. It tolerates arbitrary whitespace
// between `go`, `install`, and the module reference.
var goInstallRe = regexp.MustCompile(`go\s+install\s+([^\s@]+)@(\S+)`)

// pinnedVersionRe matches an exact released semver tag such as v3.4.1,
// optionally with a pre-release or build-metadata suffix. It intentionally
// rejects mutable selectors such as `latest`, `master`, `main`, or a partial
// version like `v3` / `v3.4`.
var pinnedVersionRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$`)

// TestReleaseWorkflowToolsPinned is a regression guard for SCB-044
// (GHSA-3xmv-x5jr-r2pq): tools installed with `go install` in the release job
// execute with that job's release credentials (contents: write plus the release
// deploy key), so they must be pinned to an exact released version. Fetching
// `@latest` (or a branch such as `@master`/`@main`) lets a compromised or
// silently updated upstream module run at release time.
func TestReleaseWorkflowToolsPinned(t *testing.T) {
	root := moduleRoot(t)
	path := filepath.Join(root, ".github", "workflows", "release.yml")

	data, err := os.ReadFile(path) //nolint:gosec // fixed path under the module root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	blocks := collectRunBlocks(strings.Split(string(data), "\n"))
	if len(blocks) == 0 {
		t.Fatalf("%s: no run: scripts detected; the scanner is broken", path)
	}

	installs := 0
	for _, block := range blocks {
		for _, m := range goInstallRe.FindAllStringSubmatch(block, -1) {
			installs++
			module, version := m[1], m[2]
			if !pinnedVersionRe.MatchString(version) {
				t.Errorf("%s: `go install %s@%s` must pin an exact released version (e.g. @v3.4.1); mutable selectors such as @latest, @master, or @main are a supply-chain risk", path, module, version)
			}
		}
	}
	if installs == 0 {
		t.Fatalf("%s: no `go install` invocations detected; the scanner is broken", path)
	}
}
