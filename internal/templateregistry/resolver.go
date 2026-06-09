package templateregistry

import (
	"os"
	"path/filepath"
)

// ResolveCachePath checks the registry cache for a template file.
// It searches all cached registries in order, returning the first match.
// Returns empty string if not found in any cache.
func ResolveCachePath(cacheDir, tplPath string) string {
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
		candidate := filepath.Join(cacheDir, registryDir, cleanPath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		// Template caches are stored as <cacheDir>/<registry>/<templateName>/<file>.
		// If not found directly under the registry dir, search one level deeper
		// within each template subdirectory.
		tplEntries, err := os.ReadDir(filepath.Dir(candidate))
		if err != nil {
			continue
		}
		for _, tplEntry := range tplEntries {
			if !tplEntry.IsDir() {
				continue
			}
			nested := filepath.Join(cacheDir, registryDir, tplEntry.Name(), cleanPath)
			if _, err := os.Stat(nested); err == nil {
				return nested
			}
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
