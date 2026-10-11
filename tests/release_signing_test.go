// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoReleaserSignsAndGeneratesSBOM is a regression guard for SCB-043: a
// release must publish an SBOM and a verifiable signature. It fails if the
// sboms/signs configuration is removed or degraded (for example signing
// individual archives instead of the checksum file, or dropping the cosign
// sigstore bundle).
func TestGoReleaserSignsAndGeneratesSBOM(t *testing.T) {
	root := moduleRoot(t)
	path := filepath.Join(root, ".goreleaser.yaml")

	data, err := os.ReadFile(path) //nolint:gosec // fixed path under the module root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	content := string(data)

	// SBOM generation for the built binaries, produced with Syft (the pipe
	// shells out to syft; the workflow installs it).
	if !hasKeyWithValue(content, "sboms:", nil) {
		t.Errorf("%s: no `sboms:` section; releases carry no SBOM", path)
	}
	if !hasKeyWithValue(content, "artifacts:", []string{"binary"}) {
		t.Errorf("%s: SBOM/sign `artifacts:` must include `binary`; found neither or the wrong value", path)
	}

	// Signature over the checksums file with cosign, emitted as a Sigstore
	// bundle so it can be verified offline.
	if !hasKeyWithValue(content, "signs:", nil) {
		t.Errorf("%s: no `signs:` section; releases are unsigned", path)
	}
	if !hasKeyWithValue(content, "cmd:", []string{"cosign"}) {
		t.Errorf("%s: `signs` must use `cmd: cosign`", path)
	}
	if !hasKeyWithValue(content, "artifacts:", []string{"checksum"}) {
		t.Errorf("%s: `signs` must sign the `checksum` file (which transitively covers every binary)", path)
	}
	if !strings.Contains(content, "--bundle=") {
		t.Errorf("%s: cosign sign-blob must use `--bundle=` to emit a Sigstore bundle", path)
	}
	if !strings.Contains(content, ".sigstore.json") {
		t.Errorf("%s: signature file must be a `.sigstore.json` bundle", path)
	}
}

// TestReleaseWorkflowAttestsProvenance is a regression guard for SCB-043: the
// release job must be allowed to mint an OIDC token and persist attestations,
// install the signing/SBOM tools, and attest build provenance for the
// published artifacts.
func TestReleaseWorkflowAttestsProvenance(t *testing.T) {
	root := moduleRoot(t)
	path := filepath.Join(root, ".github", "workflows", "release.yml")

	data, err := os.ReadFile(path) //nolint:gosec // fixed path under the module root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	content := string(data)

	// Keyless signing/attestation needs id-token to reach Sigstore, and
	// attestations to store the provenance record.
	for _, want := range []string{"id-token: write", "attestations: write"} {
		if !strings.Contains(content, want) {
			t.Errorf("%s: missing permission %q required for keyless signing/attestation", path, want)
		}
	}

	// Provenance attestation step with a subject to attest, pinned by SHA
	// (pinning itself is enforced by TestWorkflowActionsPinnedToSHA).
	attest := findUses(t, content, "actions/attest-build-provenance@")
	if attest == "" {
		t.Fatalf("%s: no `actions/attest-build-provenance` step; build provenance is not published", path)
	}
	if !strings.Contains(content, "subject-path:") {
		t.Errorf("%s: provenance attestation must set `subject-path:`", path)
	}
	// The binaries are built under dist/scbake_<os>_<arch>_<variant>/ and only
	// renamed at upload time, so the naive `dist/scbake-*` glob matches nothing.
	if !strings.Contains(content, "dist/scbake_*/scbake*") {
		t.Errorf("%s: provenance subject-path must match the build layout (dist/scbake_*/scbake*)", path)
	}
	if strings.Contains(content, "dist/scbake-*") {
		t.Errorf("%s: provenance subject-path uses dist/scbake-*, which matches no built artifact", path)
	}

	// Tools the goreleaser pipes shell out to must be installed.
	if findUses(t, content, "sigstore/cosign-installer@") == "" {
		t.Errorf("%s: cosign is not installed; the `signs` pipe cannot run", path)
	}
	if findUses(t, content, "anchore/sbom-action/download-syft@") == "" {
		t.Errorf("%s: syft is not installed; the `sboms` pipe cannot run", path)
	}
}

// TestInstallScriptVerifiesSignature is a regression guard for SCB-043: the
// installer must verify the release signature with cosign (not only a
// same-origin checksum) and document how to do so manually.
func TestInstallScriptVerifiesSignature(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "cosign verify-blob") {
		t.Error("install.sh does not verify the release signature with `cosign verify-blob`")
	}
	// A bare signature is meaningless without an identity bound to this
	// repository's release workflow and the GitHub OIDC issuer. Match the flags
	// as they appear in the real `cosign verify-blob` call (double-quoted shell
	// variables), not the single-quoted values echoed in the help text.
	if !strings.Contains(content, `--certificate-oidc-issuer "$issuer"`) {
		t.Error("install.sh does not pass --certificate-oidc-issuer \"$issuer\" to cosign verify-blob")
	}
	if !strings.Contains(content, `--certificate-identity "$identity"`) {
		t.Error("install.sh does not pass --certificate-identity \"$identity\" to cosign verify-blob")
	}
	if !strings.Contains(content, "https://token.actions.githubusercontent.com") ||
		!strings.Contains(content, ".github/workflows/release.yml@") {
		t.Error("install.sh does not pin the Sigstore issuer/identity to this repository's release workflow")
	}
	if !strings.Contains(content, "SHA256SUMS.sigstore.json") {
		t.Error("install.sh does not fetch the SHA256SUMS.sigstore.json bundle")
	}
	// The verifier must actually run during install, not merely exist.
	if !strings.Contains(content, `verify_release_signature "$VERSION"`) {
		t.Error("install.sh defines signature verification but never calls it during install")
	}
	// An escape hatch must exist to make verification mandatory.
	if !strings.Contains(content, "SCBAKE_REQUIRE_SIGNATURE") {
		t.Error("install.sh does not support requiring signature verification (SCBAKE_REQUIRE_SIGNATURE)")
	}
}

// hasKeyWithValue reports whether content contains a `key` at any indent, and,
// when values is non-empty, whether the line also carries one of those values
// after the colon (as `value` or as a YAML list item `- value`).
func hasKeyWithValue(content, key string, values []string) bool {
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, key) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, key))
		rest = strings.TrimPrefix(rest, "- ")
		if len(values) == 0 {
			return true
		}
		for _, v := range values {
			if rest == v {
				return true
			}
		}
	}
	return false
}

// findUses returns the first `uses:` value that has the given prefix, or "".
func findUses(t *testing.T, content, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		value, ok := usesValue(line)
		if ok && strings.HasPrefix(value, prefix) {
			return value
		}
	}
	return ""
}
