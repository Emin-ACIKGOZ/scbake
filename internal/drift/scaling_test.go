// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// TestDetect_LegacyFlatKeyStillResolvesThroughProjectPath pins that the #221
// scoped-key short-circuit does not disable legacy resolution: an unscoped
// pre-SCB-057 key that is absent at the manifest root is still found through
// its project path and reported clean.
func TestDetect_LegacyFlatKeyStillResolvesThroughProjectPath(t *testing.T) {
	root, m, content := legacyProject(t, types.Project{Name: "a", Path: "services/a", Language: "go"})
	writeBase(t, root, "LICENSE", content)

	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("legacy flat key resolved through its project path reported drift: %+v", got)
	}
}

// TestDetect_ScopedMissingDoesNotProbeUnrelatedProjects pins the #221
// short-circuit: a key already under a project path is Missing without trying
// <other project>/<key>, so a matching copy nested under a sibling project can
// no longer mask the deletion.
func TestDetect_ScopedMissingDoesNotProbeUnrelatedProjects(t *testing.T) {
	root := t.TempDir()
	key := "services/a/f.txt"
	content := []byte("generated\n")
	// The legacy probe would have looked for services/b/services/a/f.txt and
	// found this matching copy, making the deleted key look clean.
	mustWrite(t, filepath.Join(root, "services", "b", "services", "a", "f.txt"), content)
	writeBase(t, root, key, content)

	m := &types.Manifest{
		Projects: []types.Project{
			{Name: "a", Path: "services/a", Language: "go"},
			{Name: "b", Path: "services/b", Language: "go"},
		},
		ManagedFiles: map[string]string{key: hashOf(content)},
	}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != Missing || got[0].Path != key {
		t.Fatalf("scoped missing key = %+v, want one Missing finding for %q", got, key)
	}
}

// TestDetect_ScopedMissingFinding pins the finding shape for a deleted scoped
// key: the same single Missing entry, original path, and recorded/baseline
// hashes that detection produced before the short-circuit.
func TestDetect_ScopedMissingFinding(t *testing.T) {
	root := t.TempDir()
	key := "services/svc042/f00.txt"
	content := []byte("generated\n")
	writeBase(t, root, key, content)

	m := &types.Manifest{
		Projects:     []types.Project{{Name: "svc042", Path: "services/svc042", Language: "go"}},
		ManagedFiles: map[string]string{key: hashOf(content)},
	}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := []Finding{{
		Path:     key,
		Kind:     Missing,
		Recorded: hashOf(content),
		Baseline: hashOf(content),
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped missing findings = %+v, want %+v", got, want)
	}
}

// TestDetect_ScopedShortCircuitTradeOff pins the accepted narrowing from #221:
// a pre-SCBF-057 flat key whose leading directory coincides with another
// project's declared path is treated as scoped and reported Missing, even
// though the old legacy probe would have found the file beneath project "app".
// Skipping that probe is what keeps the scan linear; this test makes the
// trade-off intentional and reviewable.
func TestDetect_ScopedShortCircuitTradeOff(t *testing.T) {
	root := t.TempDir()
	key := "services/b/main.go"
	content := []byte("legacy main\n")
	// The old probe would have tried app/services/b/main.go and found this
	// matching copy, masking the missing root-relative key.
	mustWrite(t, filepath.Join(root, "app", "services", "b", "main.go"), content)
	writeBase(t, root, key, content)

	m := &types.Manifest{
		Projects: []types.Project{
			{Name: "a", Path: "app", Language: "go"},
			{Name: "b", Path: "services/b", Language: "go"},
		},
		ManagedFiles: map[string]string{key: hashOf(content)},
	}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != Missing || got[0].Path != key {
		t.Fatalf("trade-off fixture = %+v, want one Missing finding for %q", got, key)
	}
}

// TestDetect_ScopedMissingStillValidatesBase pins that the scoped short-circuit
// leaves legacyResolved false: a missing scoped key still compares its
// committed base, yielding Missing plus BaseMissing or BaseInvalid.
func TestDetect_ScopedMissingStillValidatesBase(t *testing.T) {
	const key = "services/a/gone.txt"
	content := []byte("generated\n")
	cases := []struct {
		name     string
		base     []byte
		wantKind Kind
	}{
		{name: "absent base", wantKind: BaseMissing},
		{name: "corrupt base", base: []byte("stale base\n"), wantKind: BaseInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.base != nil {
				writeBase(t, root, key, tc.base)
			}
			m := &types.Manifest{
				Projects:     []types.Project{{Name: "a", Path: "services/a", Language: "go"}},
				ManagedFiles: map[string]string{key: hashOf(content)},
			}

			got, err := Detect(m, root)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if len(got) != 2 {
				t.Fatalf("findings = %+v, want Missing plus %s", got, tc.wantKind)
			}
			assertScopedMissingFinding(t, got, key, content)
			assertScopedBaseFinding(t, got, key, content, tc.base, tc.wantKind)
		})
	}
}

// assertScopedMissingFinding checks the content half of a scoped missing key.
func assertScopedMissingFinding(t *testing.T, got []Finding, key string, content []byte) {
	t.Helper()
	missing := findingOfKind(t, got, Missing)
	if missing.Path != key ||
		missing.Recorded != hashOf(content) ||
		missing.Baseline != hashOf(content) ||
		missing.Actual != "" {
		t.Errorf("Missing finding = %+v, want path %q and the recorded and baseline hash", missing, key)
	}
}

// assertScopedBaseFinding checks the committed-base half of a scoped missing
// key: path, recorded hash, shadow path, and (for BaseInvalid) the base hash.
func assertScopedBaseFinding(t *testing.T, got []Finding, key string, content, base []byte, wantKind Kind) {
	t.Helper()
	f := findingOfKind(t, got, wantKind)
	if f.Path != key || f.Recorded != hashOf(content) {
		t.Errorf("%s finding = %+v, want the recorded hash", wantKind, f)
	}
	if want := filepath.ToSlash(tasks.BaseShadowRelPath(key)); f.BasePath != want {
		t.Errorf("BasePath = %q, want %q", f.BasePath, want)
	}
	if wantKind == BaseInvalid && f.Actual != hashOf(base) {
		t.Errorf("BaseInvalid Actual = %q, want the base hash", f.Actual)
	}
}

// TestDetect_AcceptedScopedMissingStillValidatesBase covers accepted_files on a
// scoped missing key: the working baseline is the accepted hash, while the
// committed base still validates against the pristine recorded hash.
func TestDetect_AcceptedScopedMissingStillValidatesBase(t *testing.T) {
	root := t.TempDir()
	key := "services/a/gone.txt"
	pristine := []byte("generated\n")
	accepted := []byte("accepted\n")
	m := &types.Manifest{
		Projects:      []types.Project{{Name: "a", Path: "services/a", Language: "go"}},
		ManagedFiles:  map[string]string{key: hashOf(pristine)},
		AcceptedFiles: map[string]string{key: hashOf(accepted)},
	}

	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want Missing plus BaseMissing", got)
	}
	missing := findingOfKind(t, got, Missing)
	if !missing.Accepted || missing.Baseline != hashOf(accepted) || missing.Recorded != hashOf(pristine) {
		t.Errorf("Missing finding = %+v, want accepted baseline and pristine recorded hash", missing)
	}
	base := findingOfKind(t, got, BaseMissing)
	if base.Recorded != hashOf(pristine) {
		t.Errorf("BaseMissing recorded = %q, want the pristine hash", base.Recorded)
	}
}

// findingOfKind returns the single finding of kind from got, failing otherwise.
func findingOfKind(t *testing.T, got []Finding, kind Kind) Finding {
	t.Helper()
	for _, f := range got {
		if f.Kind == kind {
			return f
		}
	}
	t.Fatalf("findings = %+v, want a %q finding", got, kind)
	return Finding{}
}

// scopedMissingFixture builds n projects with one missing project-scoped
// managed key each, plus its committed pristine base. It models the #221
// scaling fixture without writing the deleted working files.
func scopedMissingFixture(tb testing.TB, n int) (string, *types.Manifest) {
	tb.Helper()
	root := tb.TempDir()
	content := []byte("generated\n")
	recorded := tasks.HashContent(content)
	m := &types.Manifest{
		Projects:     make([]types.Project, 0, n),
		ManagedFiles: make(map[string]string, n),
	}
	for i := 0; i < n; i++ {
		projectPath := fmt.Sprintf("services/svc%04d", i)
		m.Projects = append(m.Projects, types.Project{
			Name:     fmt.Sprintf("svc%04d", i),
			Path:     projectPath,
			Language: "go",
		})
		key := projectPath + "/f00.txt"
		m.ManagedFiles[key] = recorded

		basePath := filepath.Join(root, tasks.BaseShadowRelPath(key))
		if err := os.MkdirAll(filepath.Dir(basePath), 0o750); err != nil {
			tb.Fatalf("mkdir base for %q: %v", key, err)
		}
		if err := os.WriteFile(basePath, content, 0o600); err != nil {
			tb.Fatalf("write base for %q: %v", key, err)
		}
	}
	return root, m
}

// BenchmarkDetectScopedMissing measures Detect with n projects and n missing
// project-scoped keys. Before #221 each missing key probed every project
// (n² path reads); the scoped short-circuit keeps the scan proportional to the
// ledger size.
func BenchmarkDetectScopedMissing(b *testing.B) {
	for _, n := range []int{250, 500, 1000, 2000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			root, m := scopedMissingFixture(b, n)
			b.ReportMetric(float64(n), "projects")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				findings, err := Detect(m, root)
				if err != nil {
					b.Fatalf("Detect: %v", err)
				}
				if len(findings) != n {
					b.Fatalf("findings = %d, want %d", len(findings), n)
				}
			}
		})
	}
}
