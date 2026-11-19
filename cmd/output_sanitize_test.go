// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// writeTerminalInjectionManifest writes a valid manifest whose project name
// contains decoded ESC sequences and whose managed_files key contains a BEL.
func writeTerminalInjectionManifest(t *testing.T, root string) {
	t.Helper()

	const body = `scbake_version = "v0.3.0"

[[projects]]
  name = "demo\u001b[2J\u001b[31mFAKE: project trusted\u001b[0m"
  path = "."
  language = "go"

[managed_files]
  "notes\u0007.txt" = "deadbeef"
`
	path := filepath.Join(root, fileutil.ManifestFileName)
	if err := os.WriteFile(path, []byte(body), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing fixture manifest: %v", err)
	}
}

// TestListProjectsEscapesTerminalControls is the regression test for #223: a
// project name from scbake.toml must be displayed escaped instead of emitting
// raw terminal control bytes.
func TestListProjectsEscapesTerminalControls(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	writeTerminalInjectionManifest(t, root)
	t.Chdir(root)

	out := captureStdout(t, func() {
		if err := executeCLI("list", "projects"); err != nil {
			t.Errorf("list projects failed: %v", err)
		}
	})

	if !strings.Contains(out, `\x1b[2J`) {
		t.Errorf("list projects did not escape ESC; stdout:\n%q", out)
	}
	if !strings.Contains(out, `\x1b[31m`) {
		t.Errorf("list projects did not escape the color sequence; stdout:\n%q", out)
	}
	if !strings.Contains(out, "FAKE: project trusted") {
		t.Errorf("list projects dropped the project name; stdout:\n%q", out)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("list projects emitted a raw ESC byte; stdout:\n%q", out)
	}
	if strings.ContainsRune(out, '\x07') {
		t.Errorf("list projects emitted a raw BEL byte; stdout:\n%q", out)
	}
}

// TestDriftEscapesTerminalControls covers a managed_files key containing BEL.
func TestDriftEscapesTerminalControls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid Windows file names")
	}
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	writeTerminalInjectionManifest(t, root)
	t.Chdir(root)

	out := captureStdout(t, func() {
		// The managed file does not exist, so drift reports a finding and
		// returns errDriftDetected after printing it.
		if err := executeCLI("drift"); err == nil {
			t.Error("drift on a missing managed file unexpectedly reported clean")
		}
	})

	if !strings.Contains(out, `notes\x07.txt`) {
		t.Errorf("drift did not escape BEL in the managed path; stdout:\n%q", out)
	}
	if !strings.Contains(out, "notes") {
		t.Errorf("drift dropped the managed path; stdout:\n%q", out)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("drift emitted a raw ESC byte; stdout:\n%q", out)
	}
	if strings.ContainsRune(out, '\x07') {
		t.Errorf("drift emitted a raw BEL byte; stdout:\n%q", out)
	}
}

// assertWrappedPathErrorSanitized pins that a filesystem PathError embedding a
// manifest-controlled path is escaped instead of reaching the terminal raw.
func assertWrappedPathErrorSanitized(t *testing.T, label, stderr string) {
	t.Helper()
	if !strings.Contains(stderr, escapedControlDirKey) &&
		!strings.Contains(stderr, goQuotedControlDirKey) {
		t.Errorf("%s did not escape the control-character path; stderr:\n%q", label, stderr)
	}
	if !strings.Contains(stderr, "\\x1b[31m") {
		t.Errorf("%s did not escape ESC; stderr:\n%q", label, stderr)
	}
	if !strings.Contains(stderr, "\\x07") && !strings.Contains(stderr, "\\a") {
		t.Errorf("%s did not escape BEL; stderr:\n%q", label, stderr)
	}
	for _, r := range []rune{'\x1b', '\x07', '\r'} {
		if strings.ContainsRune(stderr, r) {
			t.Errorf("%s emitted a raw %q byte; stderr:\n%q", label, r, stderr)
		}
	}
}

// TestDriftEscapesWrappedPathError is the regression for #223: a managed key
// naming a directory used to leak the raw ESC/BEL bytes of the *PathError from
// inspect through drift's stderr.
func TestDriftEscapesWrappedPathError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid Windows file names")
	}
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	manifest := fmt.Sprintf(`scbake_version = "v0.3.0"

[managed_files]
  "%s" = "deadbeef"
`, controlDirKeyTOML)
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte(manifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing fixture manifest: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, controlDirKey), fileutil.DirPerms); err != nil {
		t.Fatalf("creating control-character directory: %v", err)
	}
	t.Chdir(root)

	_, stderr := runWithCapturedOutput(t, func() {
		if err := executeCLI("drift"); err == nil {
			t.Error("drift on an unreadable managed path unexpectedly succeeded")
		}
	})
	assertWrappedPathErrorSanitized(t, "drift", stderr)
	if !strings.Contains(stderr, "inspecting managed file") {
		t.Errorf("drift did not report the inspect failure; stderr:\n%q", stderr)
	}
}

// TestAcceptEscapesWrappedPathError covers the accept ReadFile failure path,
// which used to wrap the same raw *PathError into the command error.
func TestAcceptEscapesWrappedPathError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid Windows file names")
	}
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	manifest := fmt.Sprintf(`scbake_version = "v0.3.0"

[managed_files]
  "%s" = "deadbeef"
`, controlDirKeyTOML)
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte(manifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing fixture manifest: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, controlDirKey), fileutil.DirPerms); err != nil {
		t.Fatalf("creating control-character directory: %v", err)
	}
	t.Chdir(root)

	_, stderr := runWithCapturedOutput(t, func() {
		if err := executeCLI("accept", controlDirKey); err == nil {
			t.Error("accept of a directory unexpectedly succeeded")
		}
	})
	assertWrappedPathErrorSanitized(t, "accept", stderr)
	if !strings.Contains(stderr, "cannot accept") {
		t.Errorf("accept did not report the read failure; stderr:\n%q", stderr)
	}
}

// TestAcceptBaseReadEscapesWrappedPathError covers the committed-base
// validation failure, whose PathError embeds the same manifest-controlled key.
func TestAcceptBaseReadEscapesWrappedPathError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid Windows file names")
	}
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	manifest := fmt.Sprintf(`scbake_version = "v0.3.0"

[managed_files]
  "%s" = "deadbeef"
`, controlDirKeyTOML)
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte(manifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing fixture manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, controlDirKey), []byte("working bytes"), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing managed file: %v", err)
	}
	basePath := filepath.Join(root, fileutil.CommittedBaseDir, controlDirKey)
	if err := os.MkdirAll(basePath, fileutil.DirPerms); err != nil {
		t.Fatalf("creating directory at committed base path: %v", err)
	}
	t.Chdir(root)

	_, stderr := runWithCapturedOutput(t, func() {
		if err := executeCLI("accept", controlDirKey); err == nil {
			t.Error("accept unexpectedly succeeded with an unreadable committed base")
		}
	})
	assertWrappedPathErrorSanitized(t, "accept base", stderr)
	if !strings.Contains(stderr, "cannot read committed base") {
		t.Errorf("accept did not report the base read failure; stderr:\n%q", stderr)
	}
}

// TestUnmanageBaseRemovalEscapesWrappedPathError forces the os.Remove failure in
// removeUnmanagedBases by making the committed base directory read-only, then
// checks that neither the base path nor the wrapped remove error leaks controls.
func TestUnmanageBaseRemovalEscapesWrappedPathError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid Windows file names")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can remove files from a read-only directory")
	}
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	manifest := fmt.Sprintf(`scbake_version = "v0.3.0"

[managed_files]
  "%s" = "deadbeef"
`, controlDirKeyTOML)
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte(manifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing fixture manifest: %v", err)
	}
	baseDir := filepath.Join(root, fileutil.CommittedBaseDir)
	if err := os.MkdirAll(baseDir, fileutil.DirPerms); err != nil {
		t.Fatalf("creating committed base directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, controlDirKey), []byte("pristine"), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing committed base: %v", err)
	}
	//nolint:gosec // G302: a read-only directory needs owner-write cleared.
	if err := os.Chmod(baseDir, 0o550); err != nil {
		t.Fatalf("making committed base directory read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(baseDir, fileutil.DirPerms) })
	t.Chdir(root)

	_, stderr := runWithCapturedOutput(t, func() {
		if err := executeCLI("unmanage", controlDirKey); err == nil {
			t.Error("unmanage unexpectedly removed a base from a read-only directory")
		}
	})
	assertWrappedPathErrorSanitized(t, "unmanage", stderr)
	if !strings.Contains(stderr, "failed to remove base shadow") {
		t.Errorf("unmanage did not report the base removal failure; stderr:\n%q", stderr)
	}
}

// TestListProjectsKeepsNormalName pins that ordinary values render unchanged.
func TestListProjectsKeepsNormalName(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	const body = `scbake_version = "v0.3.0"

[[projects]]
  name = "demo-service"
  path = "."
  language = "go"
`
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte(body), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing fixture manifest: %v", err)
	}
	t.Chdir(root)

	out := captureStdout(t, func() {
		if err := executeCLI("list", "projects"); err != nil {
			t.Errorf("list projects failed: %v", err)
		}
	})

	if !strings.Contains(out, "  - demo-service (lang: go, path: .)\n") {
		t.Errorf("list projects changed normal output; stdout:\n%q", out)
	}
}

// TestDriftSanitizesManifestHashes covers untrusted recorded and accepted hash
// values: shortHash truncates them before display, so they must be escaped
// first or a newline/ESC can forge a line or reach the terminal.
func TestDriftSanitizesManifestHashes(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	const body = `scbake_version = "v0.3.0"

[[projects]]
  name = "demo"
  path = "."
  language = "go"

[managed_files]
  "alpha.txt" = "abc\u000aFAKE: drift clean\u001b[0m"
  "beta.txt" = "plain-recorded-hash"

[accepted_files]
  "alpha.txt" = "\u001b[31mFAKE: accepted baseline"
`
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte(body), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing fixture manifest: %v", err)
	}
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("drifted content"), fileutil.PrivateFilePerms); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	t.Chdir(root)

	out := captureStdout(t, func() {
		if err := executeCLI("drift"); err == nil {
			t.Error("drift on modified managed files unexpectedly reported clean")
		}
	})

	assertDriftHashesSanitized(t, out)
}

// assertDriftHashesSanitized checks the escaped recorded/baseline values and
// rejects raw control bytes or a forged output line.
func assertDriftHashesSanitized(t *testing.T, out string) {
	t.Helper()
	// Recorded (BaseMissing for alpha) and accepted baseline (Modified for
	// alpha) both carry injected values; the accepted baseline is truncated to
	// 12 bytes first.
	if !strings.Contains(out, `abc\x0aFAKE: d`) {
		t.Errorf("drift did not escape the recorded hash; stdout:\n%q", out)
	}
	if !strings.Contains(out, `\x1b[31mFAKE: a`) {
		t.Errorf("drift did not escape the accepted baseline; stdout:\n%q", out)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("drift emitted a raw ESC byte; stdout:\n%q", out)
	}
	if strings.ContainsRune(out, '\r') {
		t.Errorf("drift emitted a raw CR byte; stdout:\n%q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "FAKE: drift clean") {
			t.Errorf("drift printed a forged line %q; stdout:\n%q", line, out)
		}
	}
}

const manageControlKey = "evil\x1b[31m\r\nFAKE: managed.txt"

// manageControlKeyTOML is manageControlKey as a TOML basic string with the
// control characters escaped.
const manageControlKeyTOML = `evil\u001b[31m\u000d\u000aFAKE: managed.txt`

// escapedManageControlKey is how Sanitize must render manageControlKey.
const escapedManageControlKey = `evil\x1b[31m\x0d\x0aFAKE: managed.txt`

// escapedManageBasePath is the sanitized committed base path for the key.
const escapedManageBasePath = `.scbake-base/` + escapedManageControlKey

// controlDirKey is a manifest key whose directory name carries ESC and BEL.
// Reading a directory fails with a PathError that embeds the key, so it
// exercises the wrapped-error display paths rather than the finding paths.
const controlDirKey = "bad\x1b[31m\x07dir"

// controlDirKeyTOML is controlDirKey as a TOML basic string.
const controlDirKeyTOML = `bad\u001b[31m\u0007dir`

// escapedControlDirKey is how Sanitize renders controlDirKey when it receives
// the raw bytes. goQuotedControlDirKey is how Go's %q renders the same path;
// a lower layer may quote a path before Sanitize sees it, in which case the
// BEL appears as the short escape \a instead. Both are safe spellings.
const (
	escapedControlDirKey  = `bad\x1b[31m\x07dir`
	goQuotedControlDirKey = `bad\x1b[31m\adir`
)

// runWithCapturedOutput runs fn and returns both captured stdout and stderr.
func runWithCapturedOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, fn)
	})
	return stdout, stderr
}

// assertSanitizedManageOutput pins the escaped form of a manifest key and base
// path and rejects raw control bytes or a forged line.
func assertSanitizedManageOutput(t *testing.T, label, out string) {
	t.Helper()
	if !strings.Contains(out, `\x1b[31m`) {
		t.Errorf("%s did not escape ESC; output:\n%q", label, out)
	}
	if !strings.Contains(out, `\x0d\x0a`) {
		t.Errorf("%s did not escape CR/LF; output:\n%q", label, out)
	}
	if !strings.Contains(out, "FAKE: managed.txt") {
		t.Errorf("%s dropped the managed key; output:\n%q", label, out)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("%s emitted a raw ESC byte; output:\n%q", label, out)
	}
	if strings.ContainsRune(out, '\r') {
		t.Errorf("%s emitted a raw CR byte; output:\n%q", label, out)
	}
	if strings.Contains(out, "\nFAKE: managed.txt") {
		t.Errorf("%s key forged a line; output:\n%q", label, out)
	}
}

// TestAcceptEscapesTerminalControls covers the key in dry-run, real, and
// already-current accept output plus the missing-base warning.
func TestAcceptEscapesTerminalControls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid Windows file names")
	}
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	manifest := fmt.Sprintf(`scbake_version = "v0.3.0"

[managed_files]
  "%s" = "deadbeef"
`, manageControlKeyTOML)
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte(manifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing fixture manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, manageControlKey), []byte("accepted bytes"), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing managed file: %v", err)
	}
	t.Chdir(root)

	dryOut, dryErr := runWithCapturedOutput(t, func() {
		if err := executeCLI("accept", "--dry-run", manageControlKey); err != nil {
			t.Errorf("accept --dry-run failed: %v", err)
		}
	})
	assertAcceptDryRunSanitized(t, dryOut, dryErr)

	resetFlags()
	realOut, realErr := runWithCapturedOutput(t, func() {
		if err := executeCLI("accept", manageControlKey); err != nil {
			t.Errorf("accept failed: %v", err)
		}
	})
	assertAcceptRealSanitized(t, realOut, realErr)

	resetFlags()
	againOut, _ := runWithCapturedOutput(t, func() {
		if err := executeCLI("accept", manageControlKey); err != nil {
			t.Errorf("repeat accept failed: %v", err)
		}
	})
	assertAcceptRepeatSanitized(t, againOut)
}

func assertAcceptDryRunSanitized(t *testing.T, out, errOut string) {
	t.Helper()
	if !strings.Contains(out, "Would accept "+escapedManageControlKey) {
		t.Errorf("dry-run accept did not escape the key; stdout:\n%q", out)
	}
	assertSanitizedManageOutput(t, "accept --dry-run", out)
	if !strings.Contains(errOut, "No committed base for "+escapedManageControlKey) {
		t.Errorf("accept warning did not escape the key; stderr:\n%q", errOut)
	}
	if strings.ContainsRune(errOut, '\x1b') || strings.ContainsRune(errOut, '\r') {
		t.Errorf("accept warning emitted a raw control byte; stderr:\n%q", errOut)
	}
}

func assertAcceptRealSanitized(t *testing.T, out, errOut string) {
	t.Helper()
	if !strings.Contains(out, "Accepted "+escapedManageControlKey) {
		t.Errorf("accept did not escape the key; stdout:\n%q", out)
	}
	assertSanitizedManageOutput(t, "accept", out)
	assertSanitizedManageOutput(t, "accept warning", errOut)
}

func assertAcceptRepeatSanitized(t *testing.T, out string) {
	t.Helper()
	if !strings.Contains(out, "already matches its current baseline") {
		t.Errorf("repeat accept did not report an already-current baseline; stdout:\n%q", out)
	}
	assertSanitizedManageOutput(t, "repeat accept", out)
}

// TestUnmanageEscapesTerminalControls covers the key and base path in dry-run,
// real, and already-unmanaged unmanage output.
func TestUnmanageEscapesTerminalControls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid Windows file names")
	}
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	manifest := fmt.Sprintf(`scbake_version = "v0.3.0"

[managed_files]
  "%s" = "deadbeef"
`, manageControlKeyTOML)
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), []byte(manifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing fixture manifest: %v", err)
	}
	basePath := filepath.Join(root, ".scbake-base", filepath.FromSlash(manageControlKey))
	if err := os.MkdirAll(filepath.Dir(basePath), fileutil.DirPerms); err != nil {
		t.Fatalf("creating base directory: %v", err)
	}
	if err := os.WriteFile(basePath, []byte("pristine bytes"), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing committed base: %v", err)
	}
	t.Chdir(root)

	dryOut, _ := runWithCapturedOutput(t, func() {
		if err := executeCLI("unmanage", "--dry-run", manageControlKey); err != nil {
			t.Errorf("unmanage --dry-run failed: %v", err)
		}
	})
	assertUnmanageDryRunSanitized(t, dryOut)

	resetFlags()
	realOut, _ := runWithCapturedOutput(t, func() {
		if err := executeCLI("unmanage", manageControlKey); err != nil {
			t.Errorf("unmanage failed: %v", err)
		}
	})
	assertUnmanageRealSanitized(t, realOut)

	resetFlags()
	againOut, _ := runWithCapturedOutput(t, func() {
		if err := executeCLI("unmanage", manageControlKey); err != nil {
			t.Errorf("repeat unmanage failed: %v", err)
		}
	})
	assertUnmanageRepeatSanitized(t, againOut)
}

func assertUnmanageDryRunSanitized(t *testing.T, out string) {
	t.Helper()
	if !strings.Contains(out, "Would unmanage "+escapedManageControlKey) {
		t.Errorf("dry-run unmanage did not escape the key; stdout:\n%q", out)
	}
	if !strings.Contains(out, "remove committed base "+escapedManageBasePath) {
		t.Errorf("dry-run unmanage did not escape the base path; stdout:\n%q", out)
	}
	assertSanitizedManageOutput(t, "unmanage --dry-run", out)
}

func assertUnmanageRealSanitized(t *testing.T, out string) {
	t.Helper()
	if !strings.Contains(out, "Unmanaged "+escapedManageControlKey) {
		t.Errorf("unmanage did not escape the key; stdout:\n%q", out)
	}
	if !strings.Contains(out, "removed committed base "+escapedManageBasePath) {
		t.Errorf("unmanage did not escape the base path; stdout:\n%q", out)
	}
	assertSanitizedManageOutput(t, "unmanage", out)
}

func assertUnmanageRepeatSanitized(t *testing.T, out string) {
	t.Helper()
	if !strings.Contains(out, "is already unmanaged") {
		t.Errorf("repeat unmanage did not report an already-unmanaged path; stdout:\n%q", out)
	}
	assertSanitizedManageOutput(t, "repeat unmanage", out)
}

// unsafeProjectNameVector is a command-line project name that passes
// templateregistry.ValidateName (a path-segment check) but is rejected later by
// core's identifier validation, after `new` has printed progress and started
// cleanup. unsafe is the rune that must never reach a stream raw.
type unsafeProjectNameVector struct {
	name    string
	raw     string
	escaped string
	unsafe  rune
}

var unsafeProjectNameVectors = []unsafeProjectNameVector{
	{name: "right-to-left override", raw: "demo\u202eevil", escaped: `demo\u202eevil`, unsafe: '\u202e'},
	{name: "line separator", raw: "demo\u2028evil", escaped: `demo\u2028evil`, unsafe: '\u2028'},
	{name: "bidi isolate", raw: "demo\u2066evil", escaped: `demo\u2066evil`, unsafe: '\u2066'},
}

// assertUnsafeNameEscapedOnce rejects a raw format control or a double-escaped
// rendering (two backslashes before the escape). The latter would mean the
// value passed through Sanitize more than once.
func assertUnsafeNameEscapedOnce(t *testing.T, label, text string, tc unsafeProjectNameVector) {
	t.Helper()
	if strings.ContainsRune(text, tc.unsafe) {
		t.Errorf("%s emitted raw %U; output:\n%q", label, tc.unsafe, text)
	}
	escapedAt := strings.IndexByte(tc.escaped, '\\')
	if escapedAt < 0 {
		t.Fatalf("test vector %q has no escape", tc.escaped)
	}
	if strings.Contains(text, `\\`+tc.escaped[escapedAt:]) {
		t.Errorf("%s double-escaped the project name; output:\n%q", label, text)
	}
}

// TestNewEscapesUnsafeProjectName is the regression test for #260: a project
// name with a bidi/line format control used to be printed raw in the progress
// line and the cleanup message before plan-time validation rejected it.
func TestNewEscapesUnsafeProjectName(t *testing.T) {
	for _, tc := range unsafeProjectNameVectors {
		t.Run(tc.name, func(t *testing.T) {
			resetFlags()
			t.Cleanup(resetFlags)

			root := t.TempDir()
			t.Chdir(root)

			var runErr error
			stdout, stderr := runWithCapturedOutput(t, func() {
				runErr = executeCLI("new", tc.raw, "--lang", "go")
			})
			if runErr == nil {
				t.Fatalf("new %q = nil, want the plan-time name rejection", tc.raw)
			}
			if !strings.Contains(stdout, "Creating directory: "+tc.escaped) {
				t.Errorf("progress line did not escape the project name; stdout:\n%q", stdout)
			}
			if !strings.Contains(stderr, "Cleaning up failed project directory '"+tc.escaped+"'...") {
				t.Errorf("cleanup line did not escape the project name; stderr:\n%q", stderr)
			}
			assertUnsafeNameEscapedOnce(t, "stdout", stdout, tc)
			assertUnsafeNameEscapedOnce(t, "stderr", stderr, tc)
			if _, err := os.Stat(filepath.Join(root, tc.raw)); !os.IsNotExist(err) {
				t.Errorf("failed new left the project directory behind (stat err: %v)", err)
			}
		})
	}
}

// TestNewExistingDirectoryEscapesProjectName covers the early error path: a
// directory that already exists is named in an error printed by the command
// layer, so the CLI-provided name must be escaped there too.
func TestNewExistingDirectoryEscapesProjectName(t *testing.T) {
	tc := unsafeProjectNameVectors[0]
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(filepath.Join(root, tc.raw), fileutil.DirPerms); err != nil {
		t.Fatalf("creating existing directory: %v", err)
	}

	var runErr error
	stdout, stderr := runWithCapturedOutput(t, func() {
		runErr = executeCLI("new", tc.raw, "--lang", "go")
	})
	if runErr == nil {
		t.Fatalf("new %q on an existing directory = nil, want an error", tc.raw)
	}
	if !strings.Contains(stderr, "directory '"+tc.escaped+"' already exists") {
		t.Errorf("existing-directory error did not escape the project name; stderr:\n%q", stderr)
	}
	assertUnsafeNameEscapedOnce(t, "stdout", stdout, tc)
	assertUnsafeNameEscapedOnce(t, "stderr", stderr, tc)
}
