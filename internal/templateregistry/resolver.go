package templateregistry

import (
	"os"
	"path/filepath"
	"sort"
)

// ResolveCacheEntry checks the registry cache for a template file belonging to
// the requested template. It searches cached registries in precedence order,
// limited to registries that are configured (registries). It returns the
// verified path and the name of the registry that supplies it, or
// ("", "") when no configured registry supplies the file.
//
// Membership is config-derived: a cached directory is only eligible when its
// name appears in registries. An unconfigured cache directory is inert even
// when it verifies against its manifest; it is never read, so deleting or
// hand-editing registries.json disables it rather than letting stale content
// shadow an embedded template. Nil and empty registries disable the registry
// tier (fail closed); there is deliberately no "scan every directory" default.
// Names that are not safe single path segments are ignored.
//
// Precedence is explicit and deterministic: registries are visited in the order
// they appear in registries, which the caller builds from the config in
// insertion order. This is independent of the registry name, so an
// alphabetically earlier registry does not silently shadow a later one. The
// cache's .registry-order.json file is deliberately ignored: it is still
// written for older binaries, but it must not override the running process's
// configured order.
//
// It only consults the verified per-template layouts
// <registry>/<templateName>/<cleanPath> and, for an archive with a single
// top-level wrapper directory named exactly like the template,
// <registry>/<templateName>/<templateName>/<cleanPath>. Any other wrapper shape
// must be selected with --subdirectory when the registry is added. Both
// candidates must resolve strictly inside the verified template directory
// before they are returned, so a ../ path can never escape it. The requested
// template name is always non-empty and constrains the lookup, preventing a
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
func ResolveCacheEntry(cacheDir, templateName, tplPath string, registries []string) (cachePath, registryName string) {
	if cacheDir == "" || ValidateName(templateName) != nil {
		return "", ""
	}

	order, ok := allowedRegistryOrder(registries)
	if !ok {
		return "", ""
	}

	dirs, err := eligibleRegistryDirs(cacheDir, order)
	if err != nil {
		return "", ""
	}

	cleanPath := filepath.Clean(tplPath)
	for _, registryDir := range dirs {
		if found := resolveInRegistry(cacheDir, registryDir, templateName, cleanPath); found != "" {
			return found, registryDir
		}
	}

	return "", ""
}

// allowedRegistryOrder maps each valid configured registry name to its position
// in registries. The first occurrence of a duplicated name wins, so the map
// rank matches the caller's configured precedence. It reports false when no
// valid name remains, which disables the registry tier (fail closed).
func allowedRegistryOrder(registries []string) (map[string]int, bool) {
	order := make(map[string]int, len(registries))
	for _, name := range registries {
		if ValidateName(name) != nil {
			continue
		}
		if _, seen := order[name]; seen {
			continue
		}
		order[name] = len(order)
	}
	return order, len(order) > 0
}

// eligibleRegistryDirs returns the cached registry directories that are
// configured, ordered by their rank in the configured allowlist. Directories
// that are not configured (including .evict-* staging paths) are discarded
// before any content is read.
func eligibleRegistryDirs(cacheDir string, order map[string]int) ([]string, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, err
	}

	dirs := make([]string, 0, len(order))
	for _, entry := range entries {
		if !entry.IsDir() || !registryEligible(entry.Name(), order) {
			continue
		}
		dirs = append(dirs, entry.Name())
	}
	sort.Slice(dirs, func(i, j int) bool {
		return order[dirs[i]] < order[dirs[j]]
	})
	return dirs, nil
}

// registryEligible reports whether a cached directory name may be consulted. A
// name is eligible only when it was a valid configured registry name, so an
// unconfigured or unsafe directory is never read.
func registryEligible(name string, order map[string]int) bool {
	if _, ok := order[name]; !ok {
		return false
	}
	return ValidateName(name) == nil
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

	// Try the flat layout first, then the same-name wrapper layout produced by
	// an archive whose single top-level directory matches the template name:
	// <registry>/<templateName>/<templateName>/<cleanPath>. Every candidate
	// must stay strictly inside the verified template directory, so neither a
	// ../ path nor a symlink can escape it.
	candidates := []string{
		filepath.Join(tplDir, cleanPath),
		filepath.Join(tplDir, templateName, cleanPath),
	}
	for _, candidate := range candidates {
		if ensureStrictlyWithin(tplDir, candidate) != nil {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
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
