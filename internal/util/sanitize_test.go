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
