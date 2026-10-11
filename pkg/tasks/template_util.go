package tasks

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"scbake/internal/templateregistry"
	"scbake/internal/util/fileutil"
)

// ReadTemplate reads a template file using a resolution chain:
//  1. overrideDir — local filesystem overrides (--template-dir or SCBAKE_TEMPLATE_DIR)
//  2. registryCacheDir — downloaded templates from remote registries
//  3. efs — embedded filesystem (built-in templates)
//
// templateName identifies which registry template the file belongs to so the
// registry cache lookup is constrained to that template. Pass the empty string
// only for embedded-only templates that never use the registry cache; an empty
// name skips the registry cache lookup entirely.
//
// If overrideDir or registryCacheDir is empty, those steps are skipped.
// Permission errors on override files are propagated; missing files fall through.
func ReadTemplate(efs embed.FS, tplPath string, overrideDir, registryCacheDir, templateName string) ([]byte, error) {
	if overrideDir != "" {
		cleanTplPath := filepath.Clean(tplPath)
		content, err := fileutil.SafeReadFile(overrideDir, cleanTplPath)
		if err == nil {
			return content, nil
		}

		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if registryCacheDir != "" {
		cachePath := templateregistry.ResolveCachePath(registryCacheDir, templateName, tplPath)
		if cachePath != "" {
			content, err := fileutil.SafeReadFile(registryCacheDir, cachePath)
			if err == nil {
				return content, nil
			}
		}
	}

	return fs.ReadFile(efs, tplPath)
}
