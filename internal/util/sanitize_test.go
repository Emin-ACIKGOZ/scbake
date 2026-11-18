// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeModuleName(t *testing.T) {
	// Dynamically determine the current directory name to verify the "." case
	// This ensures the test works even if the folder isn't named "util"
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("setup failed: could not get current working directory: %v", err)
	}
	expectedCurrentDirName := filepath.Base(cwd)

	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "Simple Lowercase",
			input:    "myproject",
			expected: "myproject",
			wantErr:  false,
		},
		{
			name:     "Spaces to Hyphens",
			input:    "My Cool Project",
			expected: "my-cool-project",
			wantErr:  false,
		},
		{
			name:     "Extract Base from Path",
			input:    "/home/user/code/Backend Service",
			expected: "backend-service",
			wantErr:  false,
		},
		{
			name:     "Handle Current Directory (.)",
			input:    ".",
			expected: expectedCurrentDirName, // Likely "util"
			wantErr:  false,
		},
		{
			name:    "Leading Dash",
			input:   "-x",
			wantErr: true,
		},
		{
			name:    "Leading Dash Flag",
			input:   "-modfile=custom.mod",
			wantErr: true,
		},
		{
			name:    "Single Dash",
			input:   "-",
			wantErr: true,
		},
		{
			name:    "Leading Space Becomes Dash",
			input:   " my app",
			wantErr: true,
		},
		{
			name:     "Leading Digit",
			input:    "123abc",
			expected: "123abc",
		},
		{
			name:     "Internal Dash",
			input:    "back-end",
			expected: "back-end",
		},
		{
			name:     "Dots Underscores and Hyphens",
			input:    "My.Project_2",
			expected: "my.project_2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeModuleName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SanitizeModuleName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("SanitizeModuleName() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain ASCII", "demo-service", "demo-service"},
		{"ordinary Unicode", "café 日本語 🚀", "café 日本語 🚀"},
		{"valid replacement char", "a\uFFFDb", "a\uFFFDb"},
		{"literal backslash sequence", `demo\x1b[2J`, `demo\x1b[2J`},
		{"ESC", "\x1b[2J\x1b[31mFAKE\x1b[0m", `\x1b[2J\x1b[31mFAKE\x1b[0m`},
		{"BEL", "notes\x07.txt", `notes\x07.txt`},
		{"CR LF TAB", "a\r\n\tb", `a\x0d\x0a\x09b`},
		{"NUL", "a\x00b", `a\x00b`},
		{"DEL", "a\x7fb", `a\x7fb`},
		{"valid C1 control", "a\u0085b", `a\x85b`},
		{"lone C1 byte", "a\x9bb", `a\x9bb`},
		{"invalid UTF-8 byte", "a\xffb", `a\xffb`},
		{"truncated multibyte sequence", "a\xe2\x82b", `a\xe2\x82b`},
		{"lone continuation byte", "a\x80b", `a\x80b`},
		{"Arabic letter mark", "a\u061cb", `a\u061cb`},
		{"left-to-right mark", "a\u200eb", `a\u200eb`},
		{"right-to-left mark", "a\u200fb", `a\u200fb`},
		{"line separator", "a\u2028b", `a\u2028b`},
		{"paragraph separator", "a\u2029b", `a\u2029b`},
		{"bidi embedding", "a\u202eb", `a\u202eb`},
		{"bidi isolate", "a\u2066b\u2069c", `a\u2066b\u2069c`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sanitize(tt.in); got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
