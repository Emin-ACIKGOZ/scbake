package templateregistry

import (
	"os"
	"path/filepath"
)

// ResolveCachePath checks the registry cache for a template file belonging to
// the requested template. It searches all cached registries in order, returning
// the first match, or the empty string if the file is not found.
//
// For each registry directory it first checks the verified per-template layout
// <registry>/<templateName>/<cleanPath> (only when templateName is non-empty),
// then falls back to the flat/legacy layout <registry>/<cleanPath>. Trying the
// per-template path first prevents an unverified flat file from shadowing a
// verified per-template file that has the same relative path. Constraining the
// nested lookup to the requested template also prevents a request for one
// template from resolving to a different template's file that happens to share
// a name.
//
// The per-template lookup is fail-closed: the template directory is only
// consulted when its integrity manifest verifies against a freshly computed
// tree hash. A poisoned directory, or a cache written before manifests existed,
// is skipped so its content is never rendered; it will be re-pulled on the next
// Pull.
//
// The flat/legacy layout is unverified legacy: it predates manifests and is
// consulted only as a fallback for backward compatibility. Files resolved
// through it carry no integrity guarantee.
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

		// Per-template layout: <registry>/<templateName>/<cleanPath>. The
		// requested template name constrains the lookup; sibling template
		// directories are never searched. The directory must verify against its
		// manifest before any file is returned, so this path is preferred over
		// the unverified flat/legacy layout.
		if templateName != "" {
			tplDir := filepath.Join(cacheDir, registryDir, templateName)
			if cacheManifestValid(tplDir) {
				nested := filepath.Join(tplDir, cleanPath)
				if _, err := os.Stat(nested); err == nil {
					return nested
				}
			}
		}

		// Flat/legacy layout: <registry>/<cleanPath>. Unverified legacy layout,
		// kept only as a fallback.
		direct := filepath.Join(cacheDir, registryDir, cleanPath)
		if _, err := os.Stat(direct); err == nil {
			return direct
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
