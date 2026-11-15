// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolveTemplateDir normalizes and validates the template override directory
// from --template-dir or SCBAKE_TEMPLATE_DIR. An empty override disables the
// override layer. Relative paths are resolved against the invocation working
// directory, before any command chdirs into a project, and the directory must
// already exist so a stale override cannot silently fall back to built-ins.
func resolveTemplateDir(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolving template directory %q: %w", raw, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("template directory %q (resolved to %q): %w", raw, abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("template directory %q (resolved to %q) is not a directory", raw, abs)
	}

	return abs, nil
}
