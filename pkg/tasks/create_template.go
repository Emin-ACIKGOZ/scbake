// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
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
func checkReconciliation(absFinalPath string, outputRelPath string, newContentHash string, tc types.TaskContext) (string, error) {
	// If force is enabled, we always overwrite the original file, ignoring state.
	if tc.Force {
		return absFinalPath, nil
	}

	// Read existing file
	//nolint:gosec // Path is canonicalized
	existingContent, err := os.ReadFile(absFinalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return absFinalPath, nil // Safe to write, file doesn't exist
		}
		return "", fmt.Errorf("failed to read existing file %s: %w", outputRelPath, err)
	}

	existingHash := HashContent(existingContent)

	// Check state
	var originalHash string
	if tc.Manifest.ManagedFiles != nil {
		originalHash = tc.Manifest.ManagedFiles[outputRelPath]
	}

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
		fmt.Printf("⚠️  Conflict in %s (user modifications detected). Writing new template to artifact.\n", outputRelPath)
		return absFinalPath + ".scbake-new", nil
	case "keep-local":
		fmt.Printf("⚠️  Conflict in %s (user modifications detected). Skipping update (--strategy=keep-local).\n", outputRelPath)
		return "", nil // Signal to skip
	case "fail":
		fallthrough
	default:
		return "", fmt.Errorf("file %s has manual modifications (drift detected). Use --conflict-strategy to resolve", outputRelPath)
	}
}

// checkFilePreconditions handles path safety and directory creation.
func checkFilePreconditions(finalPath, output, target string) error {
	// 1. Path Safety Check (Canonicalization)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("failed to resolve target path: %w", err)
	}
	absFinal, err := filepath.Abs(finalPath)
	if err != nil {
		return fmt.Errorf("failed to resolve output path: %w", err)
	}

	cleanTarget := filepath.Clean(absTarget)
	cleanFinalPath := filepath.Clean(absFinal)

	if !strings.HasPrefix(cleanFinalPath, cleanTarget) {
		return fmt.Errorf("task failed (%s): output path '%s' is outside the target path '%s'",
			filepath.Base(output), output, target)
	}

	// 2. Ensure the directory exists
	dir := filepath.Dir(cleanFinalPath)
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return nil
}

// Execute performs the template creation task.
//nolint:cyclop // Complex precondition checks and file ops are linear
func (t *CreateTemplateTask) Execute(tc types.TaskContext) (err error) {
	// 1. Read and parse the template
	tplContent, err := fs.ReadFile(t.TemplateFS, t.TemplatePath)
	if err != nil {
		return fmt.Errorf("failed to read embedded template %s: %w", t.TemplatePath, err)
	}

	tpl, err := template.New(t.TemplatePath).Funcs(templateFuncs).Parse(string(tplContent))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", t.TemplatePath, err)
	}

	// 2. Render template to memory buffer first (to calculate hash)
	data := interface{}(tc.Manifest)
	if t.TemplateData != nil {
		data = t.TemplateData
	}

	var buf bytes.Buffer
	if err = tpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to render template %s: %w", t.TemplatePath, err)
	}

	renderedBytes := buf.Bytes()
	newHash := HashContent(renderedBytes)

	// 3. Determine and check the final output path
	finalPath := filepath.Join(tc.TargetPath, t.OutputPath)
	if err = checkFilePreconditions(finalPath, t.OutputPath, tc.TargetPath); err != nil {
		return err
	}

	absPath, _ := filepath.Abs(finalPath)

	// 4. State-Aware Reconciliation
	writePath, err := checkReconciliation(absPath, t.OutputPath, newHash, tc)
	if err != nil {
		return err // Conflict strategy is 'fail'
	}
	if writePath == "" {
		return nil // Conflict strategy is 'keep-local' (skip)
	}

	if tc.DryRun {
		return nil
	}

	// 5. Safety Tracking: Register the file with the transaction manager.
	if tc.Tx != nil {
		if err := tc.Tx.Track(writePath); err != nil {
			return fmt.Errorf("failed to track file %s: %w", writePath, err)
		}
	}

	// 6. Create the output file
	//nolint:gosec
	f, err := os.OpenFile(writePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileutil.FilePerms)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", writePath, err)
	}

	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	// 7. Write the rendered buffer
	if _, err = f.Write(renderedBytes); err != nil {
		return fmt.Errorf("failed to write to %s: %w", writePath, err)
	}

	// 8. Record State
	if tc.Manifest.ManagedFiles == nil {
		tc.Manifest.ManagedFiles = make(map[string]string)
	}
	// Always record the hash against the original output path, even if we wrote an artifact
	tc.Manifest.ManagedFiles[t.OutputPath] = newHash

	return nil
}
