// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Package drift performs a read-only comparison between the managed_files
// ledger recorded in scbake.toml and the working tree, so a user or CI job can
// answer "is my generated infrastructure still what scbake produced?" without
// running a write command.
//
// A managed key may carry an accepted_files baseline: the hash of bytes the
// user explicitly accepted with `scbake accept`. Drift compares the working tree
// against that accepted hash, while the committed base under .scbake-base is
// still validated against the pristine managed_files hash. The scan remains
// read-only and lock-free either way.
package drift

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
)

// Kind classifies a managed file relative to the hash scbake recorded for it.
type Kind string

const (
	// Modified means the file still exists but its normalized hash no longer
	// matches the recorded hash.
	Modified Kind = "modified"
	// Missing means the managed file no longer exists.
	Missing Kind = "missing"
	// BaseMissing means the committed pristine base for a managed file is
	// absent from .scbake-base.
	BaseMissing Kind = "base-missing"
	// BaseInvalid means the committed pristine base exists but neither its
	// normalized nor its raw hash matches the recorded ledger hash.
	BaseInvalid Kind = "base-invalid"
)

// Finding is a single managed file that has drifted from its recorded state.
type Finding struct {
	// Path is the managed_files key: slash-separated and relative to the
	// manifest root, exactly as it is stored on disk.
	Path string
	// Kind is Modified, Missing, BaseMissing, or BaseInvalid.
	Kind Kind
	// Recorded is the EOL-insensitive SHA-256 scbake recorded when it last
	// wrote the file. It is always the pristine template hash from
	// managed_files, even when Accepted is true.
	Recorded string
	// Baseline is the hash the working tree was compared against for a content
	// finding (Modified or Missing). It equals Recorded unless the key has an
	// accepted_files entry, in which case it is the accepted working-tree
	// baseline. It is empty for base findings (BaseMissing and BaseInvalid),
	// which are about the committed base rather than the working tree.
	Baseline string
	// Accepted reports that Baseline came from accepted_files rather than the
	// pristine managed_files ledger.
	Accepted bool
	// Actual is the normalized SHA-256 of the file on disk; empty when Kind is
	// Missing or BaseMissing. For BaseInvalid it is the normalized hash of the
	// committed base.
	Actual string
	// BasePath is the root-relative slash-separated committed base path
	// expected for Path; set for BaseMissing and BaseInvalid.
	BasePath string
}

// Detect compares every managed_files entry against the working tree and the
// committed base tree, and returns the drifted files sorted by path and kind. It
// is read-only: it never writes, renders a template, opens a transaction, takes
// a lock, or touches the network, so it is safe to run in CI and cannot corrupt
// a project.
//
// A working file is clean when its on-disk normalized SHA-256 equals its
// working baseline: the accepted_files hash when one is recorded for the key,
// otherwise the managed_files hash. It is Modified when the hashes differ, and
// Missing when the path does not exist. The hash is EOL-insensitive for NUL-free
// content, so an EOL-only change is not drift. Legacy entries written before
// managed_files keys were scoped per project are resolved against the manifest's
// project paths using the same content-matching rule as the write path, so an
// unscoped legacy entry that resolves through a project path is not reported as
// drift. Scoped keys are deliberately not probed across projects (see
// inspectLegacy), so a pre-SCBF-057 flat key whose leading directory coincides
// with another project's declared path is treated as scoped and may be reported
// Missing. That narrow trade-off is accepted to keep the scan linear.
//
// For every current key the committed base under .scbake-base is also compared
// with the recorded hash: absent is BaseMissing, and a hash mismatch is
// BaseInvalid. Keys resolved only through a legacy project path are skipped
// because no version wrote a flat-key base; deleted scoped keys are still
// validated. A base read error other than ENOENT is returned to the caller so
// detection fails closed instead of misreporting an I/O problem as content
// drift. Entries with an empty recorded hash are malformed and are not treated
// as base-invalid.
func Detect(m *types.Manifest, root string) ([]Finding, error) {
	if m == nil || len(m.ManagedFiles) == 0 {
		return nil, nil
	}

	findings, err := collectFindings(m, root)
	if err != nil {
		return nil, err
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Kind < findings[j].Kind
	})
	return findings, nil
}

// collectFindings classifies every ledger entry and, for each current
// non-legacy key, validates its committed base. It only reads: inspect and
// inspectBase use the containment-safe reader, and any error aborts the scan so
// detection never reports a false clean.
func collectFindings(m *types.Manifest, root string) ([]Finding, error) {
	findings := make([]Finding, 0, len(m.ManagedFiles))
	scope := projectScope(m)
	for key, recorded := range m.ManagedFiles {
		baseline := recorded
		accepted := false
		if h := m.AcceptedFiles[key]; h != "" {
			baseline = h
			accepted = true
		}
		finding, drifted, legacyResolved, err := inspect(m, root, key, recorded, baseline, accepted, scope)
		if err != nil {
			return nil, err
		}
		if drifted {
			findings = append(findings, finding)
		}
		if legacyResolved || recorded == "" {
			continue
		}
		baseFinding, baseDrifted, err := inspectBase(root, key, recorded)
		if err != nil {
			return nil, err
		}
		if baseDrifted {
			findings = append(findings, baseFinding)
		}
	}
	return findings, nil
}

// inspect classifies a single ledger entry. drifted is false when the entry is
// clean; legacyResolved reports that the entry was found only through a legacy
// project path, so it has no committed base key to validate. A non-nil error
// means detection could not be completed and must fail closed rather than report
// a false clean.
func inspect(m *types.Manifest, root, key, recorded, baseline string, accepted bool, scope map[string]struct{}) (finding Finding, drifted, legacyResolved bool, err error) {
	data, readErr := fileutil.SafeReadFile(root, filepath.FromSlash(key))
	switch {
	case readErr == nil:
		if actual := tasks.HashContent(data); actual != baseline {
			return Finding{
				Path:     key,
				Kind:     Modified,
				Recorded: recorded,
				Baseline: baseline,
				Accepted: accepted,
				Actual:   actual,
			}, true, false, nil
		}
		return Finding{}, false, false, nil
	case errors.Is(readErr, fs.ErrNotExist):
		return inspectLegacy(m, root, key, recorded, baseline, accepted, scope)
	default:
		return Finding{}, false, false, fmt.Errorf("inspecting managed file %q: %w", key, readErr)
	}
}

// inspectLegacy resolves a key that is absent at the manifest root against the
// manifest's project paths. This is the read-only mirror of the write path's
// legacy-key migration rule: a project-relative file whose content still
// matches the recorded hash is clean. Because a flat legacy key is the baseline
// for every project that holds that filename, a divergence in any one of them
// is drift and must not be masked by a pristine copy elsewhere. No copy
// anywhere is Missing.
//
// A match through a project path reports legacyResolved: the key is a pre-scope
// flat key, and no version wrote a base at the corresponding root-relative base
// path, so base validation must be skipped to avoid a permanent false finding.
//
// A key that is already scoped under a project path is treated as Missing
// without probing. Mirroring a scoped key beneath another project is irrelevant
// for current keys and was the source of the quadratic scan. The accepted
// trade-off is that a pre-SCBF-057 flat key whose leading directory coincides
// with another project's declared path is classified as scoped and may be
// reported Missing where the legacy probe could have found a copy. Such a key
// is Missing immediately (with legacyResolved false, so its committed base is
// still validated).
func inspectLegacy(m *types.Manifest, root, key, recorded, baseline string, accepted bool, scope map[string]struct{}) (finding Finding, drifted, legacyResolved bool, err error) {
	if isProjectScoped(key, scope) {
		return Finding{
			Path:     key,
			Kind:     Missing,
			Recorded: recorded,
			Baseline: baseline,
			Accepted: accepted,
		}, true, false, nil
	}
	keyPath := filepath.FromSlash(key)
	var matched bool
	for _, p := range m.Projects {
		if p.Path == "" || p.Path == "." {
			continue // Already probed as the root-relative path.
		}
		rel := filepath.Join(filepath.FromSlash(p.Path), keyPath)
		data, readErr := fileutil.SafeReadFile(root, rel)
		if readErr != nil {
			continue
		}
		h := tasks.HashContent(data)
		if h != baseline {
			return Finding{
				Path:     key,
				Kind:     Modified,
				Recorded: recorded,
				Baseline: baseline,
				Accepted: accepted,
				Actual:   h,
			}, true, true, nil
		}
		matched = true
	}
	if matched {
		return Finding{}, false, true, nil
	}
	return Finding{
		Path:     key,
		Kind:     Missing,
		Recorded: recorded,
		Baseline: baseline,
		Accepted: accepted,
	}, true, false, nil
}

// projectScope precomputes the normalized, slash-cleaned, de-duplicated project
// paths used to detect keys that are already scoped to a project. Empty and
// root (".") project paths are skipped: they are the manifest root rather than
// a prefix. Computing the set once per scan keeps the scoped check out of the
// per-key project loop.
func projectScope(m *types.Manifest) map[string]struct{} {
	if len(m.Projects) == 0 {
		return nil
	}
	scope := make(map[string]struct{}, len(m.Projects))
	for _, p := range m.Projects {
		prefix := cleanManifestPath(p.Path)
		if prefix == "." {
			continue
		}
		scope[prefix] = struct{}{}
	}
	return scope
}

// cleanManifestPath normalizes a slash-separated manifest path for prefix
// comparison only. The original key is still used for reads and findings.
func cleanManifestPath(p string) string {
	return path.Clean(filepath.ToSlash(p))
}

// isProjectScoped reports whether key equals a project path or lies beneath
// one. It walks the key's slash-separated ancestors, so the cost depends on the
// key depth rather than the number of projects.
func isProjectScoped(key string, scope map[string]struct{}) bool {
	if len(scope) == 0 {
		return false
	}
	for cleaned := cleanManifestPath(key); ; {
		if _, ok := scope[cleaned]; ok {
			return true
		}
		i := strings.LastIndexByte(cleaned, '/')
		if i < 0 {
			return false
		}
		cleaned = cleaned[:i]
	}
}

// inspectBase validates the committed base shadow for a current ledger key.
// baseDrifted is false when the base matches the recorded hash. A missing base
// is BaseMissing; any other read error is returned so detection fails closed
// instead of hiding an I/O or containment problem as content drift.
func inspectBase(root, key, recorded string) (Finding, bool, error) {
	relPath := tasks.BaseShadowRelPath(key)
	base, err := fileutil.SafeReadFile(root, relPath)
	switch {
	case err == nil:
		if tasks.BaseMatchesRecordedHash(base, recorded) {
			return Finding{}, false, nil
		}
		return Finding{
			Path:     key,
			Kind:     BaseInvalid,
			Recorded: recorded,
			Actual:   tasks.HashContent(base),
			BasePath: filepath.ToSlash(relPath),
		}, true, nil
	case errors.Is(err, fs.ErrNotExist):
		return Finding{
			Path:     key,
			Kind:     BaseMissing,
			Recorded: recorded,
			BasePath: filepath.ToSlash(relPath),
		}, true, nil
	default:
		return Finding{}, false, fmt.Errorf("inspecting committed base for %q: %w", key, err)
	}
}
