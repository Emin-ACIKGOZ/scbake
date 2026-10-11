// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise the install.sh signature-verification path added for
// SCB-043 using the deterministic cosign stub from setupStubBinaries.

// TestInstallScriptSignatureVerified proves the installer runs cosign with the
// pinned identity and issuer, and reports success.
func TestInstallScriptSignatureVerified(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")
	cosignLog := filepath.Join(t.TempDir(), "cosign.log")
	t.Setenv("STUB_COSIGN_LOG", cosignLog)

	out, err := runInstaller(t, stubDir, "success", installDir)
	if err != nil {
		t.Fatalf("expected a successful install, got %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "Signature verified") {
		t.Errorf("expected the installer to report a verified signature, got:\n%s", out)
	}

	log, readErr := os.ReadFile(cosignLog) //nolint:gosec // test artifact
	if readErr != nil {
		t.Fatalf("read cosign log: %v", readErr)
	}
	invocation := string(log)
	for _, want := range []string{"verify-blob", "--certificate-identity", "--certificate-oidc-issuer", "SHA256SUMS"} {
		if !strings.Contains(invocation, want) {
			t.Errorf("cosign invocation is missing %q; got:\n%s", want, invocation)
		}
	}
}

// TestInstallScriptSignatureRejected proves a bad signature aborts the install
// and leaves nothing on disk.
func TestInstallScriptSignatureRejected(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")
	t.Setenv("STUB_COSIGN_MODE", "bad_signature")

	out, err := runInstaller(t, stubDir, "success", installDir)
	if err == nil {
		t.Fatalf("expected a non-zero exit on a bad signature, got success\noutput:\n%s", out)
	}
	assertNotInstalled(t, installDir)
	if !strings.Contains(out, "Signature verification failed") {
		t.Errorf("expected a signature failure message, got:\n%s", out)
	}
}

// TestInstallScriptMissingBundleNonFatal proves a tag published before signing
// was introduced (no .sigstore.json) still installs, with a warning, unless
// verification is explicitly required.
func TestInstallScriptMissingBundleNonFatal(t *testing.T) {
	stubDir := setupStubBinaries(t)
	installDir := filepath.Join(t.TempDir(), "bin")

	out, err := runInstaller(t, stubDir, "missing_signature", installDir)
	if err != nil {
		t.Fatalf("expected a missing signature bundle to be non-fatal, got %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "No signature bundle") {
		t.Errorf("expected a warning about the missing bundle, got:\n%s", out)
	}
	if _, statErr := os.Stat(installedBinaryPath(installDir)); statErr != nil {
		t.Errorf("expected the binary to be installed despite the missing bundle: %v", statErr)
	}
}
