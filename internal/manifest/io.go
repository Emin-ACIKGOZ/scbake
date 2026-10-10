// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultSbakeVersion is the manifest schema version applied when a manifest
// does not specify one (either absent or present but empty).
const DefaultSbakeVersion = "v0.3.0"

var (
	// manifestModTimes tracks the modification time when a manifest was last loaded
	// to detect concurrent modifications (optimistic locking)
	manifestModTimes = make(map[string]time.Time)
	modTimesLock     sync.Mutex
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
	data, err := fileutil.SafeReadFile(rootPath, fileutil.ManifestFileName)
	manifestPath, _ := fileutil.SafePath(rootPath, fileutil.ManifestFileName)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return a new one
			m := &types.Manifest{
				SbakeVersion: DefaultSbakeVersion,
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

	// An existing but empty (or version-less) manifest decodes to an empty
	// SbakeVersion. Apply the default so `scbake new` cannot persist an empty
	// version, matching the absent-file behavior.
	if m.SbakeVersion == "" {
		m.SbakeVersion = DefaultSbakeVersion
	}

	// Record the modification time for conflict detection during Save()
	if info, statErr := os.Stat(manifestPath); statErr == nil {
		modTimesLock.Lock()
		manifestModTimes[manifestPath] = info.ModTime()
		modTimesLock.Unlock()
	}

	return &m, rootPath, nil
}

// writeAndCloseManifestFile encodes manifest to file, syncs, and closes.
func writeAndCloseManifestFile(f *os.File, m *types.Manifest) error {
	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(m); err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}

	// Force write to disk
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync manifest to disk: %w", err)
	}

	// Close explicitly to release file handle before Rename (critical on Windows)
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp manifest: %w", err)
	}
	return nil
}

// checkManifestConflict detects if the manifest file has been modified by another process.
// Returns error if a conflict is detected.
func checkManifestConflict(manifestPath string) error {
	info, statErr := os.Stat(manifestPath)
	if statErr != nil {
		// File doesn't exist - no conflict can occur
		if os.IsNotExist(statErr) {
			return nil
		}
		// Some other stat error occurred
		return statErr
	}

	modTimesLock.Lock()
	originalModTime, exists := manifestModTimes[manifestPath]
	modTimesLock.Unlock()

	if exists && !info.ModTime().Equal(originalModTime) {
		return errors.New("manifest conflict: file was modified by another process (concurrent scbake invocation detected)")
	}
	return nil
}

// Save atomically writes the manifest to scbake.toml in the specified root path.
// It writes to a temporary file first, syncs, then renames to ensure data integrity.
// Implements optimistic locking: detects if file was modified by another process.
func Save(m *types.Manifest, rootPath string) (err error) {
	finalPath, err := fileutil.SafePath(rootPath, fileutil.ManifestFileName)
	if err != nil {
		return fmt.Errorf("invalid manifest path: %w", err)
	}
	tempPath := finalPath + ".tmp"

	// Conflict detection: Check if file has been modified by another process
	if err := checkManifestConflict(finalPath); err != nil {
		return err
	}

	// Create temp file using PrivateFilePerms (0600)
	f, err := fileutil.SafeOpenFile(rootPath, fileutil.ManifestFileName+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileutil.PrivateFilePerms)
	if err != nil {
		return fmt.Errorf("failed to create temp manifest: %w", err)
	}

	// Ensure cleanup of temp file if something goes wrong before rename.
	// We only act if there is an error, preventing double-close on success paths.
	defer func() {
		if err != nil {
			var cleanupErrors []error

			// Best effort close to ensure handle release
			if closeErr := f.Close(); closeErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to close temp file: %w", closeErr))
			}

			// Remove garbage temp file
			if removeErr := os.Remove(tempPath); removeErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to remove temp file: %w", removeErr))
			}

			// Log cleanup errors but don't overwrite original error
			if len(cleanupErrors) > 0 {
				fmt.Fprintf(os.Stderr, "⚠️  Cleanup warnings during manifest save: %v\n", errors.Join(cleanupErrors...))
			}
		}
	}()

	if err := writeAndCloseManifestFile(f, m); err != nil {
		return err
	}

	// Atomic rename
	// Note: Directory fsync is skipped here as it's generally excessive for CLI tools.
	if renameErr := os.Rename(tempPath, finalPath); renameErr != nil {
		return fmt.Errorf("failed to replace manifest file: %w", renameErr)
	}

	return nil
}
