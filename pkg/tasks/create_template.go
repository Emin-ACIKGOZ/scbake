// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/merge"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"strings"
	"text/template"
)

// templateFuncs provides utility functions for Go templates.
var templateFuncs = template.FuncMap{
	"default": func(defaultValue interface{}, given interface{}) interface{} {
		if given == nil {
			return defaultValue
		}
		switch v := given.(type) {
		case string:
			if v == "" {
				return defaultValue
			}
		case []interface{}:
			if len(v) == 0 {
				return defaultValue
			}
		}
		return given
	},
	"dict": func(values ...interface{}) (map[string]interface{}, error) {
		if len(values)%2 != 0 {
			return nil, errors.New("invalid dict call")
		}
		//nolint:mnd // Keys and values come in pairs
		dict := make(map[string]interface{}, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, ok := values[i].(string)
			if !ok {
				return nil, errors.New("dict keys must be strings")
			}
			dict[key] = values[i+1]
		}
		return dict, nil
	},
	"quote": func(s string) string {
		return fmt.Sprintf("%q", s)
	},
}

// CreateTemplateTask renders and writes a file from an embedded template.
type CreateTemplateTask struct {
	// TemplateFS is the embedded filesystem (e.g., lang.GoTemplates)
	TemplateFS embed.FS

	// TemplatePath is the path *within* the embed.FS (e.g., "go.mod.tpl")
	TemplatePath string

	// OutputPath is the destination path relative to the TargetPath (e.g., "go.mod")
	OutputPath string

	Desc string

	TaskPrio int

	// Optional: Custom data to pass to the template instead of the full manifest
	TemplateData interface{}

	// TemplateName identifies which registry template this task renders. It is
	// threaded to ReadTemplate so registry cache resolution is constrained to
	// the requested template and never reads a sibling template's file.
	TemplateName string
}

// Description implements types.Task.
func (t *CreateTemplateTask) Description() string {
	return t.Desc
}

// Priority implements types.Task.
func (t *CreateTemplateTask) Priority() int {
	return t.TaskPrio
}

// HashContent calculates the SHA-256 hash of the given bytes.
func HashContent(content []byte) string {
	hasher := sha256.New()
	hasher.Write(content)
	return hex.EncodeToString(hasher.Sum(nil))
}

// checkReconciliation evaluates drift and conflict strategies.
//
// It returns the path to write to (empty to skip), an optional replacement for
// the pending render (used by the merge strategy), and an error if the
// operation should abort.
//
// outputRelPath is the target-relative path used to read the file; ledgerKey is
// the project-scoped managed_files key used for state lookup; newContent is the
// pristine render the template is trying to write.
func checkReconciliation(
	absFinalPath, outputRelPath, ledgerKey, newContentHash string,
	newContent []byte,
	tc types.TaskContext,
) (string, []byte, error) {
	// If force is enabled, we always overwrite the original file, ignoring state.
	if tc.Force {
		return absFinalPath, nil, nil
	}

	// Read existing file
	existingContent, err := fileutil.SafeReadFile(tc.TargetPath, outputRelPath)
	if err != nil {
		if os.IsNotExist(err) {
			return absFinalPath, nil, nil // Safe to write, file doesn't exist
		}
		return "", nil, fmt.Errorf("failed to read existing file %s: %w", outputRelPath, err)
	}

	existingHash := HashContent(existingContent)

	// Check state
	originalHash := recordedHash(tc.Manifest, ledgerKey, outputRelPath, existingHash)

	// If the file exists but we've never managed it (or didn't record it), it's a conflict
	if originalHash == "" {
		p, cErr := handleConflict(absFinalPath, outputRelPath, tc.ConflictStrategy)
		return p, nil, cErr
	}

	// The template has not changed since we recorded this file. For the merge
	// strategy there is nothing to apply regardless of local edits, which is
	// what makes a second `update` a no-op. Other strategies keep the historic
	// behavior of surfacing drift.
	if newContentHash == originalHash && tc.ConflictStrategy == types.ConflictMerge {
		return "", nil, nil
	}

	// The file still matches what scbake last wrote, so it has no local drift
	// and the new template content can be written directly.
	if existingHash == originalHash {
		return absFinalPath, nil, nil
	}

	// The on-disk file already equals the pending render: converged.
	if existingHash == newContentHash {
		return absFinalPath, nil, nil
	}

	// Both the template and the file changed. Merge when asked and a pristine
	// base is available; otherwise fall back to an artifact so local edits are
	// never clobbered.
	if tc.ConflictStrategy == types.ConflictMerge {
		return reconcileMerge(absFinalPath, outputRelPath, ledgerKey, existingContent, newContent, tc)
	}

	p, cErr := handleConflict(absFinalPath, outputRelPath, tc.ConflictStrategy)
	return p, nil, cErr
}

// reconcileMerge performs the `merge` strategy: a clean three-way merge writes
// the merged content, anything else writes an artifact so no local edit is lost.
func reconcileMerge(
	absFinalPath, outputRelPath, ledgerKey string,
	existingContent, newContent []byte,
	tc types.TaskContext,
) (string, []byte, error) {
	if base, ok := readBaseShadow(tc, ledgerKey); ok {
		if merged, status := merge.Merge(base, existingContent, newContent); status == merge.Clean {
			return absFinalPath, merged, nil
		}
	}
	p, cErr := handleConflict(absFinalPath, outputRelPath, types.ConflictMerge)
	return p, nil, cErr
}

func handleConflict(absFinalPath, outputRelPath, strategy string) (string, error) {
	switch strategy {
	case types.ConflictOverwrite:
		return absFinalPath, nil
	case types.ConflictArtifact, types.ConflictMerge:
		fmt.Fprintf(os.Stderr, "⚠️  Conflict in %s (user modifications detected). Writing new template to artifact.\n", outputRelPath)
		return absFinalPath + ".scbake-new", nil
	case types.ConflictKeepLocal:
		fmt.Fprintf(os.Stderr, "⚠️  Conflict in %s (user modifications detected). Skipping update (--strategy=keep-local).\n", outputRelPath)
		return "", nil // Signal to skip
	case types.ConflictFail:
		fallthrough
	default:
		return "", fmt.Errorf("file %s has manual modifications (drift detected). Use --conflict-strategy to resolve", outputRelPath)
	}
}

// checkFilePreconditions validates that the output path stays inside the target
// directory and ensures its parent directory exists. It returns the resolved
// absolute output path.
func checkFilePreconditions(tc types.TaskContext, output, target string) (string, error) {
	absPath, err := resolveTaskPath(target, output, "output")
	if err != nil {
		return "", err
	}

	// Register implicit parents for rollback (see trackMissingAncestors).
	if err := trackMissingAncestors(tc, absPath); err != nil {
		return "", err
	}

	// Ensure the directory exists
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return absPath, nil
}

// WriteManagedFile performs state-aware reconciliation, transaction tracking,
// safe file writing, and manifest state updating for a generated file.
func WriteManagedFile(tc types.TaskContext, outputRelPath string, content []byte, perm os.FileMode) error {
	newHash := HashContent(content)

	absPath, err := checkFilePreconditions(tc, outputRelPath, tc.TargetPath)
	if err != nil {
		return err
	}

	ledgerKey := managedFilesKey(tc, absPath, outputRelPath)
	writePath, override, err := checkReconciliation(absPath, outputRelPath, ledgerKey, newHash, content, tc)
	if err != nil {
		return err // Conflict strategy is 'fail'
	}
	if writePath == "" {
		return nil // Skip (keep-local, unchanged, or a no-op)
	}

	if tc.DryRun {
		return nil
	}

	// Idempotent artifact: a repeated conflict must not rewrite a side artifact
	// whose content is already identical.
	if writePath != absPath && sameFileContent(writePath, content) {
		return nil
	}

	toWrite := content
	if override != nil {
		toWrite = override
	}
	if err := trackAndWrite(tc, writePath, toWrite, perm); err != nil {
		return err
	}

	// The ledger entry (and the pristine base shadow that mirrors it) only
	// advances when we actually wrote the canonical path. Under artifact or
	// keep-local we write elsewhere (or nowhere), so the original file keeps its
	// previous baseline (SCB-056). The recorded hash is always the pristine
	// render, never the possibly-merged content, so a merged file stays
	// recognized as locally modified.
	if writePath == absPath {
		recordManagedHash(tc.Manifest, ledgerKey, newHash)
		return writeBaseShadow(tc, ledgerKey, content)
	}
	return nil
}

// trackAndWrite registers path with the transaction and writes content to it
// through the symlink-safe helpers.
func trackAndWrite(tc types.TaskContext, path string, content []byte, perm os.FileMode) error {
	if tc.Tx != nil {
		if err := tc.Tx.Track(path); err != nil {
			return fmt.Errorf("failed to track file %s: %w", path, err)
		}
	}

	f, err := fileutil.SafeOpenFile(filepath.Dir(path), filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("failed to write to %s: %w", path, err)
	}
	return nil
}

// sameFileContent reports whether path already holds content byte-for-byte.
func sameFileContent(path string, content []byte) bool {
	existing, err := fileutil.SafeReadFile(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return false
	}
	return bytes.Equal(existing, content)
}

// taskRoot returns the absolute root a task's state lives under: the
// transaction root when present, else the absolute target path.
func taskRoot(tc types.TaskContext) string {
	if tc.Tx != nil && tc.Tx.Root() != "" {
		return tc.Tx.Root()
	}
	if tc.TargetPath == "" {
		return ""
	}
	abs, err := filepath.Abs(tc.TargetPath)
	if err != nil {
		return ""
	}
	return abs
}

// baseShadowPath returns the .scbake base path for a ledger key.
func baseShadowPath(root, ledgerKey string) (string, error) {
	rel := filepath.Join(fileutil.InternalDir, fileutil.BaseDir, filepath.FromSlash(ledgerKey))
	return fileutil.SafePath(root, rel)
}

// trackBaseAncestors registers the not-yet-existing directories of a base
// shadow with the transaction, stopping below .scbake. The transaction's own
// temp directory also lives under .scbake, so .scbake itself must never be
// registered as created: a rollback would RemoveAll it and delete the backups.
func trackBaseAncestors(tc types.TaskContext, absPath string) error {
	if tc.Tx == nil || tc.DryRun {
		return nil
	}
	root := tc.Tx.Root()
	if root == "" {
		return nil
	}
	stop := filepath.Join(root, fileutil.InternalDir)

	var missing []string
	for dir := filepath.Dir(absPath); dir != stop && isStrictlyWithin(stop, dir); dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil {
			break // First existing ancestor: everything above it already exists.
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to inspect base shadow ancestor %s: %w", dir, err)
		}
		missing = append(missing, dir)
	}
	// Register shallowest-first so reverse-order rollback removes the deepest
	// first; RemoveAll on .scbake/base is safe and leaves .scbake/tmp intact.
	for i := len(missing) - 1; i >= 0; i-- {
		if err := tc.Tx.Track(missing[i]); err != nil {
			return fmt.Errorf("failed to track base shadow ancestor %s: %w", missing[i], err)
		}
	}
	return nil
}

// writeBaseShadow stores the pristine rendered content of a managed file under
// <root>/.scbake/base/<ledgerKey>. `update` reads it back as the common ancestor
// for a three-way merge. It is tracked by the transaction so a rollback removes
// or restores it alongside the file it mirrors.
func writeBaseShadow(tc types.TaskContext, ledgerKey string, content []byte) error {
	if tc.Tx == nil || tc.DryRun || ledgerKey == "" {
		return nil
	}
	root := tc.Tx.Root()
	if root == "" {
		return nil
	}
	absPath, err := baseShadowPath(root, ledgerKey)
	if err != nil {
		return fmt.Errorf("invalid base shadow path for %s: %w", ledgerKey, err)
	}
	if err := tc.Tx.Track(absPath); err != nil {
		return fmt.Errorf("failed to track base shadow %s: %w", absPath, err)
	}
	if err := trackBaseAncestors(tc, absPath); err != nil {
		return err
	}
	return writeBaseFile(absPath, content)
}

// writeBaseFile creates the base shadow's parent directory and writes content.
func writeBaseFile(absPath string, content []byte) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create base shadow directory %s: %w", dir, err)
	}
	f, err := fileutil.SafeOpenFile(dir, filepath.Base(absPath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileutil.PrivateFilePerms)
	if err != nil {
		return fmt.Errorf("failed to create base shadow %s: %w", absPath, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("failed to write base shadow %s: %w", absPath, err)
	}
	return nil
}

// readBaseShadow returns the pristine base content for a managed file, if it
// exists.
func readBaseShadow(tc types.TaskContext, ledgerKey string) ([]byte, bool) {
	if ledgerKey == "" {
		return nil, false
	}
	root := taskRoot(tc)
	if root == "" {
		return nil, false
	}
	rel := filepath.Join(fileutil.InternalDir, fileutil.BaseDir, filepath.FromSlash(ledgerKey))
	data, err := fileutil.SafeReadFile(root, rel)
	if err != nil {
		return nil, false
	}
	return data, true
}

// recordManagedHash stores the SHA-256 hash of a file scbake wrote at key in the
// manifest's managed_files ledger, initializing the map when necessary.
func recordManagedHash(m *types.Manifest, key, hash string) {
	if m.ManagedFiles == nil {
		m.ManagedFiles = make(map[string]string)
	}
	m.ManagedFiles[key] = hash
}

// managedFilesKey derives the managed_files ledger key for a file: the path
// relative to the transaction's project root. Scoping by the root (rather than
// the target project) keeps monorepo subprojects that generate the same
// relative filename from aliasing each other's entry. Without a transaction
// manager, or when the file lies outside its root, it falls back to the
// target-relative path.
func managedFilesKey(tc types.TaskContext, absPath, outputRelPath string) string {
	if tc.Tx == nil {
		return outputRelPath
	}
	root := tc.Tx.Root()
	if root == "" {
		return outputRelPath
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return outputRelPath
	}
	// Normalize to forward slashes so keys are identical across platforms and
	// match the slash-separated paths templates declare.
	return filepath.ToSlash(rel)
}

// recordedHash returns the hash scbake last recorded for a managed path. It
// looks up the project-scoped key first. For manifests written before keys were
// scoped, it falls back to the legacy project-relative key only when that hash
// matches the file's current on-disk content.
//
// This is the migration rule: an unchanged scbake-generated file is recognized
// and re-scoped. A mismatched legacy key (for example a stale entry left by
// another subproject) is not used as a drift baseline. Note the inherent limit:
// if an unmanaged file's bytes happen to equal a surviving legacy hash, the two
// cases are indistinguishable and the file is treated as managed.
func recordedHash(m *types.Manifest, ledgerKey, legacyKey, existingHash string) string {
	if m == nil || m.ManagedFiles == nil {
		return ""
	}
	if h := m.ManagedFiles[ledgerKey]; h != "" {
		return h
	}
	if ledgerKey == legacyKey {
		return ""
	}
	if legacy := m.ManagedFiles[legacyKey]; legacy != "" && legacy == existingHash {
		return legacy
	}
	return ""
}

// Execute performs the template creation task.
func (t *CreateTemplateTask) Execute(tc types.TaskContext) error {
	// 1. Read and parse the template using the override-aware helper
	tplContent, err := ReadTemplate(t.TemplateFS, t.TemplatePath, tc.TemplateDir, tc.RegistryCacheDir, t.TemplateName)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", t.TemplatePath, err)
	}

	tpl, err := template.New(t.TemplatePath).Funcs(templateFuncs).Parse(string(tplContent))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", t.TemplatePath, err)
	}

	// 2. Render template to memory buffer first
	data := interface{}(tc.Manifest)
	if t.TemplateData != nil {
		data = t.TemplateData
	}

	var buf bytes.Buffer
	if err = tpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to render template %s: %w", t.TemplatePath, err)
	}

	// 3. Delegate state reconciliation, transaction tracking, and writing to WriteManagedFile
	return WriteManagedFile(tc, t.OutputPath, buf.Bytes(), fileutil.FilePerms)
}
