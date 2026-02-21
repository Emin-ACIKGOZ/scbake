// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package manifest provides functions for reading and writing the scbake manifest file (scbake.toml).
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"

	"github.com/BurntSushi/toml"
)

// FindProjectRoot looks for scbake.toml or .git starting in startPath and walking up.
// It returns the directory containing the marker, or the startPath (normalized directory) if not found.
func FindProjectRoot(startPath string) (string, error) {
	// 1. Normalize startPath to an absolute directory once.
	// This serves as our starting point and our fallback.
	startDir, err := filepath.Abs(startPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// If the input is a file (e.g. "main.go"), start from its directory.
	info, err := os.Stat(startDir)
	if err == nil && !info.IsDir() {
		startDir = filepath.Dir(startDir)
	}

	current := startDir

	for {
		// 2. Check for scbake.toml (Primary Marker)
		manifestPath := filepath.Join(current, fileutil.ManifestFileName)
		if _, err := os.Stat(manifestPath); err == nil {
			return current, nil
		}

		// 3. Check for .git (Secondary Marker)
		// This helps in monorepos where scbake.toml might not exist yet (init phase)
		// but we still want to respect the git root.
		gitPath := filepath.Join(current, fileutil.GitDir)
		if _, err := os.Stat(gitPath); err == nil {
			return current, nil
		}

		// 4. Move up one level
		parent := filepath.Dir(current)

		// 5. Root Detection: If parent is same as current, we hit the FS root.
		if parent == current {
			break
		}
		current = parent
	}

	// Fallback: If no marker found, return the normalized start directory.
	// This supports running 'scbake new' in a fresh, empty directory.
	return startDir, nil
}

// Load reads scbake.toml from the project root discovered from startPath.
// If not found, it returns a new, empty manifest.
// It returns the Manifest and the discovered Root Path.
func Load(startPath string) (*types.Manifest, string, error) {
	rootPath, err := FindProjectRoot(startPath)
	if err != nil {
		return nil, "", err
	}

	// Try to read the file at the discovered root
	manifestPath := filepath.Join(rootPath, fileutil.ManifestFileName)

	// G304: The path is constructed from user input but sanitized via filepath.Join/Abs.
	// Reading the manifest file from the target directory is the intended behavior of this CLI tool.
	//nolint:gosec // Intended file read
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return a new one
			m := &types.Manifest{
				SbakeVersion: "v0.0.1", // TODO: inject this from build flags
				Projects:     []types.Project{},
				Templates:    []types.Template{},
			}
			return m, rootPath, nil
		}
		// Some other error
		return nil, "", err
	}

	var m types.Manifest
	if _, err := toml.Decode(string(data), &m); err != nil {
		return nil, "", fmt.Errorf("failed to decode manifest: %w", err)
	}

	return &m, rootPath, nil
}

// Save atomically writes the manifest to scbake.toml in the specified root path.
// It writes to a temporary file first, syncs, then renames to ensure data integrity.
func Save(m *types.Manifest, rootPath string) (err error) {
	finalPath := filepath.Join(rootPath, fileutil.ManifestFileName)
	tempPath := finalPath + ".tmp"

	// Create temp file using PrivateFilePerms (0600)
	// G304: The path is constructed from user input but sanitized.
	// Creating the temp manifest file in the target directory is intended behavior.
	//nolint:gosec // Intended file creation
	f, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileutil.PrivateFilePerms)
	if err != nil {
		return fmt.Errorf("failed to create temp manifest: %w", err)
	}

	// Ensure cleanup of temp file if something goes wrong before rename.
	// We only act if there is an error, preventing double-close on success paths.
	defer func() {
		if err != nil {
			// Best effort close (ignoring error) to ensure handle release
			_ = f.Close()
			// Remove garbage temp file
			_ = os.Remove(tempPath)
		}
	}()

	encoder := toml.NewEncoder(f)
	if encodeErr := encoder.Encode(m); encodeErr != nil {
		return fmt.Errorf("failed to encode manifest: %w", encodeErr)
	}

	// Force write to disk
	if syncErr := f.Sync(); syncErr != nil {
		return fmt.Errorf("failed to sync manifest to disk: %w", syncErr)
	}

	// Close explicitly to release file handle before Rename (critical on Windows).
	// If this fails, 'err' becomes non-nil, and the defer will attempt cleanup.
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("failed to close temp manifest: %w", closeErr)
	}

	// Atomic rename
	// Note: Directory fsync is skipped here as it's generally excessive for CLI tools.
	if renameErr := os.Rename(tempPath, finalPath); renameErr != nil {
		return fmt.Errorf("failed to replace manifest file: %w", renameErr)
	}

	return nil
}
