// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpringStubArchive(t *testing.T, path string) {
	t.Helper()

	//nolint:gosec // Test temp directory
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating stub archive: %v", err)
	}
	zw := zip.NewWriter(out)
	for name, content := range map[string]string{
		"pom.xml": "<project><modelVersion>4.0.0</modelVersion><build><plugins></plugins></build></project>",
		"mvnw":    "",
	} {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatalf("creating %s in stub archive: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("writing %s in stub archive: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing stub archive: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("closing stub archive file: %v", err)
	}
}

// installSpringStubBinaries provides deterministic curl and unzip stand-ins so
// the spring plan can produce pom.xml without network access. curl copies a
// valid archive because the task tracks its entries before running. unzip also
// emits the Maven wrapper the following chmod task expects.
func installSpringStubBinaries(t *testing.T) string {
	t.Helper()

	binDir := t.TempDir()
	archivePath := filepath.Join(binDir, "spring.zip")
	writeSpringStubArchive(t, archivePath)

	scripts := map[string]string{
		"curl": fmt.Sprintf(`#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o) out="$2"; shift 2 ;;
		*) shift ;;
	esac
done
if [ -n "$out" ]; then
	cp '%s' "$out"
fi
exit 0
`, archivePath),
		"unzip": `#!/bin/sh
cat > pom.xml <<'POM'
<project><modelVersion>4.0.0</modelVersion><build><plugins></plugins></build></project>
POM
: > mvnw
exit 0
`,
	}

	for name, content := range scripts {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil { //nolint:gosec // test stub must be executable
			t.Fatalf("writing %s stub: %v", name, err)
		}
	}
	return binDir
}

// TestDryRunSpringMavenParity is a regression for #144: maven_linter inserts
// into a pom.xml that the earlier spring plan produces. Its Validate must not
// fail the dry run just because the file does not exist yet.
func TestDryRunSpringMavenParity(t *testing.T) {
	binDir := installSpringStubBinaries(t)

	cases := []struct {
		name    string
		args    []string
		prepare func(t *testing.T, root string) (workDir, projectDir string)
	}{
		{
			name: "new",
			args: []string{"new", "app", "--lang", "spring", "--with", "maven_linter"},
			prepare: func(_ *testing.T, root string) (string, string) {
				return root, filepath.Join(root, "app")
			},
		},
		{
			name: "apply",
			args: []string{"apply", "--lang", "spring", "--with", "maven_linter"},
			prepare: func(t *testing.T, root string) (string, string) {
				t.Helper()
				if out, err := runScbake(t, root, binDir, "new", "app", "--with", "makefile"); err != nil {
					t.Fatalf("preparing apply fixture: %v\n%s", err, out)
				}
				project := filepath.Join(root, "app")
				return project, project
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dryRoot := t.TempDir()
			dryWork, _ := tc.prepare(t, dryRoot)
			before := dryRunTakeSnapshot(t, dryRoot)
			dryArgs := append(append([]string{}, tc.args...), "--dry-run")
			dryOut, dryErr := runScbake(t, dryWork, binDir, dryArgs...)
			if dryErr != nil {
				t.Fatalf("dry run = %v, want nil\n%s", dryErr, dryOut)
			}
			after := dryRunTakeSnapshot(t, dryRoot)
			dryRunAssertSnapshotsEqual(t, before, after)
			dryRunAssertNoResidue(t, after)

			realRoot := t.TempDir()
			realWork, projectDir := tc.prepare(t, realRoot)
			realOut, realErr := runScbake(t, realWork, binDir, tc.args...)
			if realErr != nil {
				t.Fatalf("real run = %v, want nil\n%s", realErr, realOut)
			}

			pomPath := filepath.Join(projectDir, "pom.xml")
			pom, err := os.ReadFile(filepath.Clean(pomPath))
			if err != nil {
				t.Fatalf("reading pom.xml: %v", err)
			}
			if got := strings.Count(string(pom), "maven-checkstyle-plugin"); got != 1 {
				t.Errorf("pom.xml has %d maven-checkstyle-plugin entries, want exactly 1", got)
			}
			if _, err := os.Stat(filepath.Join(projectDir, "checkstyle.xml")); err != nil {
				t.Errorf("real run did not create checkstyle.xml: %v", err)
			}
		})
	}
}
