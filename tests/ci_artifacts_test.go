// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ciArtifact describes one uploaded artifact and the attestation step that must
// precede it.
type ciArtifact struct {
	job          string
	attestStep   string
	subject      string
	uploadStep   string
	artifact     string
	artifactPath string
}

// TestCIAttestsUploadedArtifacts is a regression guard for issue #52
// (SCB-054): the coverage report and the cross-compiled binaries uploaded by
// CI must carry a SLSA build-provenance attestation. GitHub only mints an OIDC
// token and grants `attestations: write` for trusted runs, so fork and
// Dependabot PRs must skip the step instead of failing the job.
func TestCIAttestsUploadedArtifacts(t *testing.T) {
	root := moduleRoot(t)
	path := filepath.Join(root, ".github", "workflows", "ci.yml")
	lines := readWorkflowLines(t, path)

	assertWorkflowPermissions(t, path, lines)
	for _, tc := range ciArtifacts() {
		assertAttestedArtifact(t, path, lines, tc)
	}
}

func ciArtifacts() []ciArtifact {
	return []ciArtifact{
		{
			job:          "coverage",
			attestStep:   "Attest coverage artifact",
			subject:      "coverage.out",
			uploadStep:   "Upload coverage artifact",
			artifact:     "coverage",
			artifactPath: "coverage.out",
		},
		{
			job:          "build",
			attestStep:   "Attest binary artifact",
			subject:      "dist/scbake-${{ matrix.goos }}-${{ matrix.goarch }}*",
			uploadStep:   "Upload binary artifact",
			artifact:     "scbake-${{ matrix.goos }}-${{ matrix.goarch }}",
			artifactPath: "dist/scbake-${{ matrix.goos }}-${{ matrix.goarch }}*",
		},
	}
}

func readWorkflowLines(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // fixed path under the module root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(string(data), "\n")
}

// assertWorkflowPermissions requires the workflow default to be exactly
// `contents: read`. Elevated permissions belong on the artifact jobs only.
func assertWorkflowPermissions(t *testing.T, path string, lines []string) {
	t.Helper()

	block := yamlBlock(lines, "permissions:")
	if len(block) == 0 {
		t.Fatalf("%s: workflow-level permissions block is missing; it must grant only contents: read", path)
	}

	entries := 0
	for _, line := range block[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed != "contents: read" {
			t.Errorf("%s: workflow-level permission %q is not allowed; the default must be exactly contents: read", path, trimmed)
			continue
		}
		entries++
	}
	if entries != 1 {
		t.Errorf("%s: workflow-level permissions must declare contents: read exactly once; found %d", path, entries)
	}
}

func assertAttestedArtifact(t *testing.T, path string, lines []string, tc ciArtifact) {
	t.Helper()

	job := yamlBlock(lines, "  "+tc.job+":")
	if len(job) == 0 {
		t.Fatalf("%s: job %q not found; the scanner is broken", path, tc.job)
	}

	assertJobPermissions(t, path, tc.job, job)
	assertAttestStep(t, path, job, tc)
	assertUploadStep(t, path, job, tc)
}

func assertJobPermissions(t *testing.T, path, jobName string, job []string) {
	t.Helper()

	permissions := yamlBlock(job, "    permissions:")
	for _, want := range []string{"contents: read", "id-token: write", "attestations: write"} {
		if !hasLine(permissions, want) {
			t.Errorf("%s: job %q is missing permission %q for build-provenance attestation", path, jobName, want)
		}
	}
}

func assertAttestStep(t *testing.T, path string, job []string, tc ciArtifact) {
	t.Helper()

	step := yamlBlock(job, "      - name: "+tc.attestStep)
	if len(step) == 0 {
		t.Fatalf("%s: job %q has no %q step", path, tc.job, tc.attestStep)
	}

	uses := findUses(t, strings.Join(step, "\n"), "actions/attest-build-provenance@")
	if uses == "" || !actionPinned(uses) {
		t.Errorf("%s: %q must use actions/attest-build-provenance pinned to a full commit SHA; got %q", path, tc.attestStep, uses)
	}
	if got := yamlScalar(t, step, "subject-path"); got != tc.subject {
		t.Errorf("%s: %q subject-path = %q; want %q", path, tc.attestStep, got, tc.subject)
	}
	if hasKey(step, "continue-on-error") {
		t.Errorf("%s: %q must not set continue-on-error; a failed attestation must fail the job", path, tc.attestStep)
	}
	assertAttestGuard(t, path, tc, step)
}

// assertAttestGuard pins the guard's operator structure. A substring check
// alone would accept flipping the outer `||` to `&&`, which skips every run.
func assertAttestGuard(t *testing.T, path string, tc ciArtifact, step []string) {
	t.Helper()

	condition := yamlScalar(t, step, "if")
	for _, want := range []string{
		"github.event_name != 'pull_request'",
		"github.event.pull_request.head.repo.full_name == github.repository",
		"github.event.pull_request.user.login != 'dependabot[bot]'",
		"github.actor != 'dependabot[bot]'",
	} {
		if !strings.Contains(condition, want) {
			t.Errorf("%s: %q condition must contain %q; got %q", path, tc.attestStep, want, condition)
		}
	}
	if got := normalizeSpace(condition); got != attestGuardExpression {
		t.Errorf("%s: %q condition = %q; want %q", path, tc.attestStep, got, attestGuardExpression)
	}
}

const attestGuardExpression = "github.event_name != 'pull_request' || " +
	"(github.event.pull_request.head.repo.full_name == github.repository && " +
	"github.event.pull_request.user.login != 'dependabot[bot]' && " +
	"github.actor != 'dependabot[bot]')"

func assertUploadStep(t *testing.T, path string, job []string, tc ciArtifact) {
	t.Helper()

	uploadHeader := "      - name: " + tc.uploadStep
	upload := yamlBlock(job, uploadHeader)
	if len(upload) == 0 {
		t.Fatalf("%s: job %q lost its %q step", path, tc.job, tc.uploadStep)
	}
	if got := yamlScalar(t, upload, "name"); got != tc.artifact {
		t.Errorf("%s: %q artifact name = %q; want %q", path, tc.uploadStep, got, tc.artifact)
	}
	if got := yamlScalar(t, upload, "path"); got != tc.artifactPath {
		t.Errorf("%s: %q artifact path = %q; want %q", path, tc.uploadStep, got, tc.artifactPath)
	}

	// Publication must keep GitHub's implicit success() condition. An explicit
	// condition could run the upload after a failed attestation and publish an
	// un-attested artifact, defeating the fail-closed ordering above.
	if hasKey(upload, "if") {
		t.Errorf("%s: %q must not set if:; a conditional upload could publish an un-attested artifact", path, tc.uploadStep)
	}
	if hasKey(upload, "continue-on-error") {
		t.Errorf("%s: %q must not set continue-on-error; a failed upload must fail the job", path, tc.uploadStep)
	}

	// The upload must run last: a failed attestation has to block publication.
	attestAt := blockStart(job, "      - name: "+tc.attestStep)
	if attestAt < 0 {
		t.Fatalf("%s: job %q has no %q step", path, tc.job, tc.attestStep)
	}
	if attestAt >= blockStart(job, uploadHeader) {
		t.Errorf("%s: %q must precede %q so a failed attestation blocks the upload", path, tc.attestStep, tc.uploadStep)
	}
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// blockStart returns the index of header in lines, or -1 when absent.
func blockStart(lines []string, header string) int {
	for i, line := range lines {
		if strings.TrimRight(line, " \t") == header {
			return i
		}
	}
	return -1
}

// yamlBlock returns header and the lines nested under it, stopping at the next
// non-empty line at the same or lower indentation. This is a deliberately small
// YAML subset scanner for the plain block mappings/sequences used here; adding
// a full YAML parser solely for a test would add a module dependency.
func yamlBlock(lines []string, header string) []string {
	start := blockStart(lines, header)
	if start < 0 {
		return nil
	}

	base := indentOf(lines[start])
	block := []string{lines[start]}
	for _, line := range lines[start+1:] {
		if strings.TrimSpace(line) != "" && indentOf(line) <= base {
			break
		}
		block = append(block, line)
	}
	return block
}

// yamlScalar returns the value of a scalar key in block, joining the
// continuation lines of a folded/literal block scalar with spaces.
func yamlScalar(t *testing.T, block []string, key string) string {
	t.Helper()

	prefix := key + ":"
	for i, line := range block {
		trimmed := strings.TrimSpace(line)
		if trimmed != prefix && !strings.HasPrefix(trimmed, prefix+" ") {
			continue
		}

		value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if value != "" && !blockScalarRe.MatchString(value) {
			return value
		}

		base := indentOf(line)
		var parts []string
		for _, next := range block[i+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			if indentOf(next) <= base {
				break
			}
			parts = append(parts, strings.TrimSpace(next))
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func hasLine(block []string, want string) bool {
	for _, line := range block {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// hasKey reports whether block declares key, regardless of its value.
func hasKey(block []string, key string) bool {
	prefix := key + ":"
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		if trimmed == prefix || strings.HasPrefix(trimmed, prefix+" ") {
			return true
		}
	}
	return false
}
