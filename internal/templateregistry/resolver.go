package templateregistry

import (
	"os"
	"path/filepath"
)

// ResolveCachePath checks the registry cache for a template file belonging to
// the requested template. It searches all cached registries in order, returning
// the first match, or the empty string if the file is not found.
//
// For each registry directory it first checks the flat/legacy layout
// <registry>/<cleanPath>, then — only when templateName is non-empty — the
// per-template layout <registry>/<templateName>/<cleanPath>. Constraining the
// nested lookup to the requested template prevents a request for one template
// from resolving to a different template's file that happens to share a name.
func ResolveCachePath(cacheDir, templateName, tplPath string) string {
	if cacheDir == "" {
		return ""
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return ""
	}

	cleanPath := filepath.Clean(tplPath)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		registryDir := entry.Name()

		// Flat/legacy layout: <registry>/<cleanPath>.
		direct := filepath.Join(cacheDir, registryDir, cleanPath)
		if _, err := os.Stat(direct); err == nil {
			return direct
		}

		// Per-template layout: <registry>/<templateName>/<cleanPath>. The
		// requested template name constrains the lookup; sibling template
		// directories are never searched.
		if templateName == "" {
			continue
		}
		nested := filepath.Join(cacheDir, registryDir, templateName, cleanPath)
		if _, err := os.Stat(nested); err == nil {
			return nested
		}
	}

	return ""
}

// LookupPath returns the full path to a template file in the cache for a
// specific registry. Returns empty string if not found.
func LookupPath(cacheDir, registryName, tplPath string) string {
	if cacheDir == "" || registryName == "" {
		return ""
	}
	candidate := filepath.Join(cacheDir, registryName, filepath.Clean(tplPath))
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
}
