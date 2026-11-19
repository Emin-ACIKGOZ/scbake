// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runIssue260 executes the compiled binary in dir and keeps stdout and stderr
// separate, which the `list` stream-separation regression depends on.
func runIssue260(t *testing.T, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	//nolint:gosec // test-controlled binary and arguments
	cmd := exec.CommandContext(context.Background(), binaryPath, args...)
	cmd.Dir = dir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// TestIssue260TemplateOnlyAllowsPathSafeNames preserves the pre-fix behavior for
// template-only projects: spaces and ordinary Unicode are valid directory names
// even though language runs normalize/validate them differently.
func TestIssue260TemplateOnlyAllowsPathSafeNames(t *testing.T) {
	for _, name := range []string{"my project", "café"} {
		t.Run(name, func(t *testing.T) {
			work := t.TempDir()
			t.Chdir(work)
			if out, err := runCLI("new", name, "--with", "makefile"); err != nil {
				t.Fatalf("template-only new rejected path-safe name %q: %v\n%s", name, err, out)
			}
			if _, err := os.Stat(filepath.Join(work, name)); err != nil {
				t.Errorf("project directory %q was not created: %v", name, err)
			}
		})
	}
}

// TestIssue260RegistryErrorsEscapeNames closes the remaining same-class paths:
// unknown --with and --lang names are echoed by the registries before any
// template renders, so they must be escaped there too.
func TestIssue260RegistryErrorsEscapeNames(t *testing.T) {
	tests := []struct {
		name string
		args []string
		raw  []rune
		want string
	}{
		{
			name: "unknown template",
			args: []string{"new", "demo", "--with", "bogus\x1b]0;x\x07"},
			raw:  []rune{'\x1b', '\x07'},
			want: `bogus\x1b]0;x\x07`,
		},
		{
			name: "unknown language",
			args: []string{"new", "demo", "--lang", "go\u009bX"},
			raw:  []rune{'\u009b'},
			want: `go\x9bX`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work := t.TempDir()
			t.Chdir(work)
			out, err := runCLI(tt.args...)
			if err == nil {
				t.Fatalf("expected an unknown-name error:\n%s", out)
			}
			for _, r := range tt.raw {
				if strings.ContainsRune(out, r) {
					t.Errorf("output emitted raw %U:\n%q", r, out)
				}
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("output does not contain the escaped name %q:\n%s", tt.want, out)
			}
		})
	}
}

// TestIssue260NewTemplateOnlyRejectsUnsafeName closes the follow-up gap: a
// template-only `new` skipped language planning, so an unsafe name reached the
// filesystem. It must now be rejected before creation, with escaped output.
func TestIssue260NewTemplateOnlyRejectsUnsafeName(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	name := "demo\u202eevil"

	out, err := runCLI("new", name, "--with", "makefile")
	if err == nil {
		t.Fatalf("new accepted an unsafe template-only name:\n%s", out)
	}
	if strings.ContainsRune(out, '\u202e') {
		t.Errorf("output contains a raw U+202E:\n%q", out)
	}
	if _, statErr := os.Stat(filepath.Join(work, name)); !os.IsNotExist(statErr) {
		t.Errorf("rejected new left a directory behind (stat err: %v)", statErr)
	}
}

// TestIssue260ListUnknownResourceSanitized is the regression for #260: an
// unknown `list` resource argument used to be echoed raw to stderr and the hint
// describing valid values used to go to stdout. Both streams are asserted so
// the split cannot regress under `2>/dev/null`.
func TestIssue260ListUnknownResourceSanitized(t *testing.T) { //nolint:cyclop // table-driven stream contract
	tests := []struct {
		name    string
		arg     string
		escaped string
		forged  string
		raw     []rune
	}{
		{
			name:    "OSC title with BEL",
			arg:     "bogus\x1b]0;x\x07",
			escaped: `bogus\x1b]0;x\x07`,
			raw:     []rune{'\x1b', '\x07'},
		},
		{
			name:    "forged line",
			arg:     "bogus\nFAKE: list clean",
			escaped: `bogus\x0aFAKE: list clean`,
			forged:  "FAKE: list clean",
			raw:     []rune{'\r'},
		},
		{
			name:    "C1 control",
			arg:     "bogus\u009bx",
			escaped: `bogus\x9bx`,
			raw:     []rune{'\u009b'},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := runIssue260(t, t.TempDir(), "list", tc.arg)
			if err == nil {
				t.Fatalf("list %q = nil, want an unknown-resource error", tc.arg)
			}

			if stdout != "" {
				t.Errorf("unknown-resource output leaked to stdout: %q", stdout)
			}
			if !strings.Contains(stderr, "Unknown resource type '"+tc.escaped+"'.") {
				t.Errorf("error did not escape the resource argument; stderr:\n%q", stderr)
			}
			if !strings.Contains(stderr, "Must be one of: langs, templates, projects\n") {
				t.Errorf("hint is not on stderr; stderr:\n%q", stderr)
			}
			for _, r := range tc.raw {
				if strings.ContainsRune(stderr, r) {
					t.Errorf("stderr emitted raw %U; stderr:\n%q", r, stderr)
				}
			}
			if strings.Contains(stderr, tc.arg) {
				t.Errorf("stderr echoed the raw resource argument; stderr:\n%q", stderr)
			}
			if tc.forged != "" {
				for _, line := range strings.Split(stderr, "\n") {
					if strings.HasPrefix(line, tc.forged) {
						t.Errorf("resource argument forged a line %q; stderr:\n%q", line, stderr)
					}
				}
			}
			if strings.Contains(stderr, `\\`+tc.escaped[strings.IndexByte(tc.escaped, '\\'):]) {
				t.Errorf("resource argument was double-escaped; stderr:\n%q", stderr)
			}
		})
	}
}

// TestIssue260NewUnsafeNameSanitized is the regression for #260: a project name
// with a right-to-left override used to reach the progress line and cleanup
// message raw before core validation rejected it.
func TestIssue260NewUnsafeNameSanitized(t *testing.T) {
	const name = "demo\u202eevil"
	const escaped = `demo\u202eevil`

	dir := t.TempDir()
	stdout, stderr, err := runIssue260(t, dir, "new", name, "--lang", "go")
	if err == nil {
		t.Fatalf("new %q = nil, want the plan-time name rejection", name)
	}

	if !strings.Contains(stdout, "Creating directory: "+escaped) {
		t.Errorf("progress line did not escape the project name; stdout:\n%q", stdout)
	}
	if !strings.Contains(stderr, "Cleaning up failed project directory '"+escaped+"'...") {
		t.Errorf("cleanup line did not escape the project name; stderr:\n%q", stderr)
	}
	for label, stream := range map[string]string{"stdout": stdout, "stderr": stderr} {
		if strings.ContainsRune(stream, '\u202e') {
			t.Errorf("%s emitted a raw U+202E; output:\n%q", label, stream)
		}
		if strings.Contains(stream, `\\u202e`) {
			t.Errorf("%s double-escaped the project name; output:\n%q", label, stream)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
		t.Errorf("failed new left the project directory behind (stat err: %v)", statErr)
	}
}
