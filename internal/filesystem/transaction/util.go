// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"io"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
)

// copyFile copies a file from src to dst, preserving the specified mode.
// Paths passed here are already validated by Manager.Track to ensure they
// reside within the configured project root.
func copyFile(src, dst string, mode os.FileMode) (err error) {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	dir := filepath.Dir(src)
	base := filepath.Base(src)
	sourceFile, err := fileutil.SafeOpenFile(dir, base, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := sourceFile.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	// Create destination with the original mode
	dstDir := filepath.Dir(dst)
	dstBase := filepath.Base(dst)
	destFile, err := fileutil.SafeOpenFile(dstDir, dstBase, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := destFile.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err = io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Explicitly chmod ensuring the file mode is exactly what we want
	// (OpenFile's mode is affected by umask, Chmod is not)
	if err = os.Chmod(dst, mode); err != nil {
		return err
	}

	return nil
}
