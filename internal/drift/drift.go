// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Package drift performs a read-only comparison between the managed_files
// ledger recorded in scbake.toml and the working tree, so a user or CI job can
// answer "is my generated infrastructure still what scbake produced?" without
// running a write command.
package drift

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
)

// Kind classifies a managed file relative to the hash scbake recorded for it.
type Kind string

const (
	// Modified means the file still exists but no longer matches its recorded hash.
	Modified Kind = "modified"
	// Missing means the managed file no longer exists.
	Missing Kind = "missing"
)

// Finding is a single managed file that has drifted from its recorded state.
type Finding struct {
	// Path is the managed_files key: slash-separated and relative to the
	// manifest root, exactly as it is stored on disk.
	Path string
	// Kind is Modified or Missing.
	Kind Kind
	// Recorded is the SHA-256 scbake recorded when it last wrote the file.
	Recorded string
	// Actual is the SHA-256 of the file on disk; empty when Kind is Missing.
	Actual string
}

// Detect compares every managed_files entry against the working tree and
// returns the drifted files sorted by path. It is read-only: it never writes,
// renders a template, opens a transaction, or touches the network, so it is
// safe to run in CI and cannot corrupt a project.
//
// A file is clean when its on-disk SHA-256 equals the recorded hash, Modified
// when the bytes differ, and Missing when the path does not exist. Legacy
// entries written before managed_files keys were scoped per project are
// resolved against the manifest's project paths using the same
// content-matching rule as the write path, so detection never reports false
// drift for a file scbake still manages.
func Detect(m *types.Manifest, root string) ([]Finding, error) {
	if m == nil || len(m.ManagedFiles) == 0 {
		return nil, nil
	}

	findings := make([]Finding, 0, len(m.ManagedFiles))
	for key, recorded := range m.ManagedFiles {
		finding, drifted, err := inspect(m, root, key, recorded)
		if err != nil {
			return nil, err
		}
		if drifted {
			findings = append(findings, finding)
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].Path < findings[j].Path })
	return findings, nil
}

// inspect classifies a single ledger entry. drifted is false when the entry is
// clean (or a recognized legacy entry); a non-nil error means detection could
// not be completed and must fail closed rather than report a false clean.
func inspect(m *types.Manifest, root, key, recorded string) (Finding, bool, error) {
	data, err := fileutil.SafeReadFile(root, filepath.FromSlash(key))
	switch {
	case err == nil:
		if actual := tasks.HashContent(data); actual != recorded {
			return Finding{Path: key, Kind: Modified, Recorded: recorded, Actual: actual}, true, nil
		}
		return Finding{}, false, nil
	case errors.Is(err, fs.ErrNotExist):
		return inspectLegacy(m, root, key, recorded)
	default:
		return Finding{}, false, fmt.Errorf("inspecting managed file %q: %w", key, err)
	}
}

// inspectLegacy resolves a key that is absent at the manifest root against the
// manifest's project paths. This is the read-only mirror of the write path's
// legacy-key migration rule: a project-relative file whose content still
// matches the recorded hash is clean. A project-relative file that exists but
// no longer matches is Modified; no file anywhere is Missing.
func inspectLegacy(m *types.Manifest, root, key, recorded string) (Finding, bool, error) {
	keyPath := filepath.FromSlash(key)
	var actual string
	for _, p := range m.Projects {
		if p.Path == "" || p.Path == "." {
			continue // Already probed as the root-relative path.
		}
		rel := filepath.Join(filepath.FromSlash(p.Path), keyPath)
		data, err := fileutil.SafeReadFile(root, rel)
		if err != nil {
			continue
		}
		h := tasks.HashContent(data)
		if h == recorded {
			return Finding{}, false, nil
		}
		actual = h
	}
	if actual != "" {
		return Finding{Path: key, Kind: Modified, Recorded: recorded, Actual: actual}, true, nil
	}
	return Finding{Path: key, Kind: Missing, Recorded: recorded}, true, nil
}
