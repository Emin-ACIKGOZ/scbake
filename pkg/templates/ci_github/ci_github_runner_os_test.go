// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cighub

import (
	"strings"
	"testing"

	"scbake/internal/types"
)

// TestWorkflow_RunnerOSRenders covers the ci_github consumer for #140.
func TestWorkflow_RunnerOSRenders(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]string
		want     string
	}{
		{name: "windows", metadata: map[string]string{"runner_os": "windows-latest"}, want: "\n    runs-on: windows-latest\n"},
		{name: "ubuntu", metadata: map[string]string{"runner_os": "ubuntu-latest"}, want: "\n    runs-on: ubuntu-latest\n"},
		{name: "macos", metadata: map[string]string{"runner_os": "macos-latest"}, want: "\n    runs-on: macos-latest\n"},
		{name: "nil_metadata", metadata: nil, want: "\n    runs-on: ubuntu-latest\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &types.Manifest{
				Projects: []types.Project{{Name: "backend", Path: "backend", Language: "go"}},
				Metadata: tc.metadata,
			}
			out := renderWorkflow(t, m)
			if !strings.Contains(out, tc.want) {
				t.Errorf("workflow missing %q:\n%s", tc.want, out)
			}
			if strings.Contains(out, `runs-on: "`) {
				t.Errorf("runs-on must stay an unquoted scalar:\n%s", out)
			}
		})
	}
}

// TestWorkflow_DefaultRunnerBytesIdentical pins the compatibility requirement:
// an absent runner_os and an explicit ubuntu-latest must render the exact same
// bytes as the pre-#140 hard-coded template.
func TestWorkflow_DefaultRunnerBytesIdentical(t *testing.T) {
	project := []types.Project{{Name: "backend", Path: "backend", Language: "go"}}

	nilMetadata := renderWorkflow(t, &types.Manifest{Projects: project})
	explicitDefault := renderWorkflow(t, &types.Manifest{
		Projects: project,
		Metadata: map[string]string{"runner_os": "ubuntu-latest"},
	})

	if nilMetadata != explicitDefault {
		t.Fatalf("default runner rendering changed bytes:\n--- nil metadata ---\n%s\n--- ubuntu-latest ---\n%s",
			nilMetadata, explicitDefault)
	}
	if !strings.Contains(nilMetadata, "\n    runs-on: ubuntu-latest\n") {
		t.Fatalf("default workflow missing the unquoted ubuntu-latest runner:\n%s", nilMetadata)
	}
}
