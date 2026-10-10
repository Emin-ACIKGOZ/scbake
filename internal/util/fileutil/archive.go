// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package fileutil

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ExtractTarGz extracts a gzipped tarball stream into destDir while enforcing
// path containment safety (Zip Slip prevention) and maximum read byte limits (Zip Bomb protection).
func ExtractTarGz(r io.Reader, destDir string, maxBytes int64) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("initializing gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("reading tar header: %w", err)
		}

		if err := extractTarEntry(tr, header, destDir, maxBytes); err != nil {
			return err
		}
	}

	return nil
}

// extractTarEntry processes a single tar header entry safely.
func extractTarEntry(tr *tar.Reader, header *tar.Header, destDir string, maxBytes int64) error {
	// Sanitize and verify target path containment inside destDir (Zip Slip protection)
	safePath, err := SafePath(destDir, header.Name)
	if err != nil {
		return fmt.Errorf("path traversal rejected for %s: %w", header.Name, err)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(safePath, DirPerms); err != nil {
			return fmt.Errorf("creating directory %s: %w", safePath, err)
		}
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(safePath), DirPerms); err != nil {
			return fmt.Errorf("creating directory for %s: %w", safePath, err)
		}

		dir := filepath.Dir(safePath)
		base := filepath.Base(safePath)
		f, err := SafeOpenFile(dir, base, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, PrivateFilePerms)
		if err != nil {
			return fmt.Errorf("creating file %s: %w", safePath, err)
		}
		defer func() { _ = f.Close() }()

		if _, err := io.Copy(f, io.LimitReader(tr, maxBytes)); err != nil {
			return fmt.Errorf("writing file %s: %w", safePath, err)
		}
	}

	return nil
}
