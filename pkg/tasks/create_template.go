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

	// Human-readable description
	Desc string

	// Execution priority
	TaskPrio int

	// Optional: Custom data to pass to the template instead of the full manifest
	TemplateData interface{}
}

// Description returns a human-readable summary of the task.
func (t *CreateTemplateTask) Description() string {
	return t.Desc
}

// Priority returns the execution priority level.
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
// Returns the path to write to, or an error if the operation should abort.
//
// outputRelPath is the target-relative path used to read the file; ledgerKey is
// the project-scoped managed_files key used for state lookup.
func checkReconciliation(
	absFinalPath, outputRelPath, ledgerKey, newContentHash string,
	tc types.TaskContext,
) (string, error) {
	// If force is enabled, we always overwrite the original file, ignoring state.
	if tc.Force {
		return absFinalPath, nil
	}

	// Read existing file
	existingContent, err := fileutil.SafeReadFile(tc.TargetPath, outputRelPath)
	if err != nil {
		if os.IsNotExist(err) {
			return absFinalPath, nil // Safe to write, file doesn't exist
		}
		return "", fmt.Errorf("failed to read existing file %s: %w", outputRelPath, err)
	}

	existingHash := HashContent(existingContent)

	// Check state
	originalHash := recordedHash(tc.Manifest, ledgerKey, outputRelPath)

	// If the file exists but we've never managed it (or didn't record it), it's a conflict
	if originalHash == "" {
		return handleConflict(absFinalPath, outputRelPath, tc.ConflictStrategy)
	}

	// If the file exactly matches what we originally generated, it hasn't drifted. Safe to update.
	if existingHash == originalHash {
		return absFinalPath, nil
	}

	// If the existing file happens to already match what we're trying to generate, we're good (idempotent).
	if existingHash == newContentHash {
		return absFinalPath, nil
	}

	// Drift detected! The user modified the file.
	return handleConflict(absFinalPath, outputRelPath, tc.ConflictStrategy)
}

func handleConflict(absFinalPath, outputRelPath, strategy string) (string, error) {
	switch strategy {
	case "overwrite":
		return absFinalPath, nil
	case "artifact":
		fmt.Fprintf(os.Stderr, "⚠️  Conflict in %s (user modifications detected). Writing new template to artifact.\n", outputRelPath)
		return absFinalPath + ".scbake-new", nil
	case "keep-local":
		fmt.Fprintf(os.Stderr, "⚠️  Conflict in %s (user modifications detected). Skipping update (--strategy=keep-local).\n", outputRelPath)
		return "", nil // Signal to skip
	case "fail":
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

	// Register the parent directories MkdirAll is about to create implicitly so
	// that a rollback removes them along with the file itself.
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
	writePath, err := checkReconciliation(absPath, outputRelPath, ledgerKey, newHash, tc)
	if err != nil {
		return err // Conflict strategy is 'fail'
	}
	if writePath == "" {
		return nil // Conflict strategy is 'keep-local' (skip)
	}

	if tc.DryRun {
		return nil
	}

	if tc.Tx != nil {
		if err := tc.Tx.Track(writePath); err != nil {
			return fmt.Errorf("failed to track file %s: %w", writePath, err)
		}
	}

	dir := filepath.Dir(writePath)
	base := filepath.Base(writePath)
	f, err := fileutil.SafeOpenFile(dir, base, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", writePath, err)
	}
	defer func() { _ = f.Close() }()

	if _, err = f.Write(content); err != nil {
		return fmt.Errorf("failed to write to %s: %w", writePath, err)
	}

	// Only record the ledger entry when we actually wrote the content to the
	// reconciled original path. Under --conflict-strategy=artifact we write to
	// a "<path>.scbake-new" sibling, leaving the original file untouched, so its
	// previously recorded hash must be preserved.
	if writePath == absPath {
		recordManagedHash(tc.Manifest, ledgerKey, newHash)
	}
	return nil
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
// looks up the project-scoped key first and falls back to the legacy
// project-relative key so manifests written before keys were scoped keep
// working.
func recordedHash(m *types.Manifest, ledgerKey, legacyKey string) string {
	if m == nil || m.ManagedFiles == nil {
		return ""
	}
	if h := m.ManagedFiles[ledgerKey]; h != "" {
		return h
	}
	if ledgerKey != legacyKey {
		return m.ManagedFiles[legacyKey]
	}
	return ""
}

// Execute performs the template creation task.
func (t *CreateTemplateTask) Execute(tc types.TaskContext) error {
	// 1. Read and parse the template using the override-aware helper
	tplContent, err := ReadTemplate(t.TemplateFS, t.TemplatePath, tc.TemplateDir, tc.RegistryCacheDir)
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
