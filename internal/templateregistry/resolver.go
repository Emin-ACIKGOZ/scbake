package templateregistry

import (
	"os"
	"path/filepath"
)

// ResolveCachePath checks the registry cache for a template file belonging to
// the requested template. It searches cached registries in precedence order,
// returning the first match, or the empty string if the file is not found.
//
// Precedence is explicit and deterministic: registries are visited in the order
// they were added to the config, as recorded in the cache's order file. This is
// independent of the registry name, so an alphabetically earlier registry does
// not silently shadow a later one. Registries not present in the order file are
// visited in lexical order after the recorded ones. If the order file is
// missing (for example a cache written before it existed), the fallback is
// lexical order, matching the previous behavior.
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
	if ValidateName(templateName) != nil {
		return ""
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return ""
	}

	registryDirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			registryDirs = append(registryDirs, entry.Name())
		}
	}

	cleanPath := filepath.Clean(tplPath)
	for _, registryDir := range orderRegistries(cacheDir, registryDirs) {
		if found := resolveInRegistry(cacheDir, registryDir, templateName, cleanPath); found != "" {
			return found
		}
	}

	return ""
}

// resolveInRegistry returns the verified path of cleanPath within the requested
// template directory of a single cached registry, or the empty string when the
// registry name is unsafe, the directory does not verify against its manifest,
// or the file is absent.
func resolveInRegistry(cacheDir, registryDir, templateName, cleanPath string) string {
	if ValidateName(registryDir) != nil {
		return ""
	}

	// Per-template layout: <registry>/<templateName>/<cleanPath>. The
	// requested template name constrains the lookup; sibling template
	// directories are never searched. The directory must verify against its
	// manifest before any file is returned.
	tplDir := filepath.Join(cacheDir, registryDir, templateName)
	if !cacheManifestValid(tplDir) {
		return ""
	}
	nested := filepath.Join(tplDir, cleanPath)
	if _, err := os.Stat(nested); err == nil {
		return nested
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
