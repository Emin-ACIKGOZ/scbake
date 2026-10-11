package templateregistry

import (
	"os"
	"path/filepath"
)

// ResolveCachePath checks the registry cache for a template file belonging to
// the requested template. It searches all cached registries in order, returning
// the first match, or the empty string if the file is not found.
//
// It only consults the verified per-template layout
// <registry>/<templateName>/<cleanPath> and requires templateName to be
// non-empty. Constraining the lookup to the requested template prevents a
// request for one template from resolving to a different template's file that
// happens to share a name.
//
// The lookup is fail-closed: the template directory is only consulted when its
// integrity manifest verifies against a freshly computed tree hash. A poisoned
// directory, a cache written before manifests existed, or an empty template
// tree is skipped so its content is never rendered; it will be re-pulled on the
// next Pull.
//
// There is deliberately no flat/legacy fallback: Pull and PullFromURL have
// always written the per-template layout, and an unverified <registry>/<cleanPath>
// file could otherwise be planted locally and rendered, defeating the manifest
// check.
func ResolveCachePath(cacheDir, templateName, tplPath string) string {
	if cacheDir == "" || templateName == "" {
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
		// manifest before any file is returned.
		tplDir := filepath.Join(cacheDir, registryDir, templateName)
		if cacheManifestValid(tplDir) {
			nested := filepath.Join(tplDir, cleanPath)
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
