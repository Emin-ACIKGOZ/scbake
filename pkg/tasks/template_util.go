package tasks

import (
	"embed"
	"errors"
	"io/fs"
	"path/filepath"
	"scbake/internal/templateregistry"
	"scbake/internal/util/fileutil"
)

// Template source tiers returned by ResolveTemplateSource.
const (
	// SourceOverride is a local file from --template-dir or SCBAKE_TEMPLATE_DIR.
	SourceOverride = "template-dir"
	// SourceRegistry is a manifest-verified file from a configured registry's cache.
	SourceRegistry = "registry"
	// SourceEmbedded is a compiled-in template.
	SourceEmbedded = "built-in"
)

// TemplateSource identifies the tier that supplied (or would supply) a
// template file.
type TemplateSource struct {
	// Tier is one of SourceOverride, SourceRegistry, or SourceEmbedded.
	Tier string
	// Registry is the supplying registry name when Tier is SourceRegistry.
	Registry string
}

// Label returns the user-facing name of the source tier, for example
// "built-in", "template-dir", or "registry: acme".
func (s TemplateSource) Label() string {
	if s.Tier == SourceRegistry && s.Registry != "" {
		return "registry: " + s.Registry
	}
	return s.Tier
}

// ReadTemplate reads a template file using a resolution chain:
//  1. overrideDir — local filesystem overrides (--template-dir or SCBAKE_TEMPLATE_DIR)
//  2. registryCacheDir — downloaded templates from configured remote registries
//  3. efs — embedded filesystem (built-in templates)
//
// templateName identifies which registry template the file belongs to so the
// registry cache lookup is constrained to that template. Pass the empty string
// only for embedded-only templates that never use the registry cache; an empty
// name skips the registry cache lookup entirely.
//
// registries is the configured registry allowlist. Only cached directories
// named in registries are consulted, so an unconfigured registry's cache can
// never shadow an embedded template. Nil or empty disables the registry tier
// (fail closed); there is no "scan every directory" fallback.
//
// If overrideDir or registryCacheDir is empty, those steps are skipped.
// Permission errors on override files are propagated; missing files fall through.
func ReadTemplate(efs embed.FS, tplPath string, overrideDir, registryCacheDir, templateName string, registries []string) ([]byte, error) {
	content, _, err := readTemplate(efs, tplPath, overrideDir, registryCacheDir, templateName, registries)
	return content, err
}

// ResolveTemplateSource reports the tier that ReadTemplate would use for the
// given arguments, without changing the resolution rules. It returns the zero
// TemplateSource when no tier can supply the file (for example, a missing
// embedded path). It is used for dry-run disclosure.
func ResolveTemplateSource(efs embed.FS, tplPath string, overrideDir, registryCacheDir, templateName string, registries []string) TemplateSource {
	_, source, err := readTemplate(efs, tplPath, overrideDir, registryCacheDir, templateName, registries)
	if err != nil {
		return TemplateSource{}
	}
	return source
}

// readTemplate implements ReadTemplate and reports the source tier alongside
// the bytes so display and execution share one resolver.
func readTemplate(efs embed.FS, tplPath string, overrideDir, registryCacheDir, templateName string, registries []string) ([]byte, TemplateSource, error) {
	if overrideDir != "" {
		cleanTplPath := filepath.Clean(tplPath)
		content, err := fileutil.SafeReadFile(overrideDir, cleanTplPath)
		if err == nil {
			return content, TemplateSource{Tier: SourceOverride}, nil
		}

		// errors.Is unwraps the %w chain emitted by SafePath/canonicalize, so a
		// dangling symlink (reported as os.ErrNotExist) still falls through.
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, TemplateSource{}, err
		}
	}

	if registryCacheDir != "" {
		cachePath, registryName := templateregistry.ResolveCacheEntry(registryCacheDir, templateName, tplPath, registries)
		if cachePath != "" {
			content, err := fileutil.SafeReadFile(registryCacheDir, cachePath)
			if err == nil {
				return content, TemplateSource{Tier: SourceRegistry, Registry: registryName}, nil
			}
		}
	}

	content, err := fs.ReadFile(efs, tplPath)
	if err != nil {
		return nil, TemplateSource{}, err
	}
	return content, TemplateSource{Tier: SourceEmbedded}, nil
}
