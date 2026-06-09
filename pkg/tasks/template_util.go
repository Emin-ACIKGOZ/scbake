package tasks

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// ReadTemplate reads a template file from an external override directory first.
// If overrideDir is empty or the file doesn't exist there, it falls back to
// reading from the embedded filesystem (efs). Permission errors on the override
// file are propagated; missing files silently fall through to the embedded copy.
func ReadTemplate(efs embed.FS, tplPath string, overrideDir string) ([]byte, error) {
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

	return fs.ReadFile(efs, tplPath)
}
