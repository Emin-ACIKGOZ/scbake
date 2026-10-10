// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Package testutil provides shared helper utilities for internal unit and integration testing.
package testutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
)

const defaultTestFileMode int64 = 0644

// CreateTarGz constructs an in-memory gzipped tarball byte slice from a file path-to-content map.
func CreateTarGz(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: defaultTestFileMode,
		}
		_ = tw.WriteHeader(hdr)
		_, _ = tw.Write([]byte(content))
	}

	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}
