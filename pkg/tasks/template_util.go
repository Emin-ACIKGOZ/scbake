package tasks

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"scbake/internal/templateregistry"
)

// ReadTemplate reads a template file using a resolution chain:
//  1. overrideDir — local filesystem overrides (--template-dir or SCBAKE_TEMPLATE_DIR)
//  2. registryCacheDir — downloaded templates from remote registries
//  3. efs — embedded filesystem (built-in templates)
//
// If overrideDir or registryCacheDir is empty, those steps are skipped.
// Permission errors on override files are propagated; missing files fall through.
func ReadTemplate(efs embed.FS, tplPath string, overrideDir, registryCacheDir string) ([]byte, error) {
	if overrideDir != "" {
		cleanTplPath := filepath.Clean(tplPath)
		overridePath := filepath.Join(overrideDir, cleanTplPath)

		//nolint:gosec // overrideDir is user-provided and trusted
		content, err := os.ReadFile(overridePath)
		if err == nil {
			return content, nil
		}

		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if registryCacheDir != "" {
		cachePath := templateregistry.ResolveCachePath(registryCacheDir, tplPath)
		if cachePath != "" {
			//nolint:gosec // cachePath is within our controlled cache dir
			content, err := os.ReadFile(cachePath)
			if err == nil {
				return content, nil
			}
		}
	}

	return fs.ReadFile(efs, tplPath)
}
