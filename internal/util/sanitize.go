package util

import (
	"path/filepath"
	"strings"
)

// SanitizeModuleName creates a valid Go module name from a path.
// It takes the base name, lowercases it, and replaces spaces with hyphens.
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
	// More sanitization can be added here (e.g., removing special chars)

	return name, nil
}
