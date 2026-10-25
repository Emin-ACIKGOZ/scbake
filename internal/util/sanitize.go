// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SanitizeModuleName creates a valid Go module name from a path.
// It takes the base name, lowercases it, and replaces spaces with hyphens.
// It returns an error if the resulting name starts with a dash, which
// `go mod init` would parse as a flag.
func SanitizeModuleName(path string) (string, error) {
	// Get the base name (e.g., "my cool app")
	name := filepath.Base(path)
	if name == "." || name == "/" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		name = filepath.Base(abs)
	}

	// Sanitize the name
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")

	// Reject rather than normalize, so the name is not silently altered.
	if strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("invalid module name %q: leading dash", name)
	}

	return name, nil
}
