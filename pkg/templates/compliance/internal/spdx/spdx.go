// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Package spdx provides the embedded SPDX license texts used by the
// compliance template. The data is pinned to the SPDX License List version
// reported by Version, so license generation is deterministic and works
// without network access.
package spdx

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"strings"
	"sync"
)

//go:generate go run ./gen

//go:embed licenses.zip
var licenseArchive []byte

var (
	loadOnce sync.Once
	loadErr  error
	files    map[string]*zip.File
)

// Text returns the canonical license text for the SPDX identifier id.
//
// Matching is case-insensitive. The returned text may contain SPDX
// placeholders such as <year> and <copyright holders>; callers are
// responsible for substitution.
func Text(id string) (string, error) {
	canonical, ok := canonicalIDs[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return "", fmt.Errorf("unknown SPDX license ID %q (SPDX License List %s)", id, Version)
	}

	if err := load(); err != nil {
		return "", fmt.Errorf("open embedded SPDX license data: %w", err)
	}

	entry, ok := files["licenses/"+canonical+".txt"]
	if !ok {
		return "", fmt.Errorf("embedded SPDX license data is missing %q", canonical)
	}

	reader, err := entry.Open()
	if err != nil {
		return "", fmt.Errorf("read embedded SPDX license %q: %w", canonical, err)
	}
	defer func() { _ = reader.Close() }()

	text, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read embedded SPDX license %q: %w", canonical, err)
	}

	return string(text), nil
}

func load() error {
	loadOnce.Do(func() {
		archive, err := zip.NewReader(bytes.NewReader(licenseArchive), int64(len(licenseArchive)))
		if err != nil {
			loadErr = err
			return
		}

		files = make(map[string]*zip.File, len(archive.File))
		for _, file := range archive.File {
			files[file.Name] = file
		}
	})

	return loadErr
}
