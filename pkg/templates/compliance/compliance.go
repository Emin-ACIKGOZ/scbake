// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package compliance provides templates for enterprise security and compliance.
package compliance

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"strconv"
	"strings"
	"time"

	"github.com/mitchellh/go-spdx"
)

//go:embed templates/*.tpl
var templates embed.FS

// Handler implements the templates.Handler interface for compliance.
type Handler struct{}

// GetTasks returns the plan to create compliance files.
func (h *Handler) GetTasks(_ string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequence for the Compliance band (1200-1399 range, PrioLinter)
	seq, err := types.NewPrioritySequence(types.PrioLinter, types.MaxLinter)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	// 1. SECURITY.md
	p, _ := seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "templates/SECURITY.md.tpl",
		OutputPath:   "SECURITY.md",
		Desc:         "Create SECURITY.md",
		TaskPrio:     int(p),
	})

	// 2. dependabot.yml (in .github/)
	p, _ = seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "templates/dependabot.yml.tpl",
		OutputPath:   ".github/dependabot.yml",
		Desc:         "Create dependabot.yml",
		TaskPrio:     int(p),
	})

	// 3. LICENSE (Dynamic)
	// We check for Metadata in the Manifest. If missing, this task will fail during execution.
	p, _ = seq.Next()
	plan = append(plan, &LicenseTask{
		TaskPrio: int(p),
	})

	// 4. CODEOWNERS (Surgical Append)
	p, _ = seq.Next()
	plan = append(plan, &tasks.AppendFileTask{
		FilePath: ".github/CODEOWNERS",
		Content:  "# Managed by scbake\n* @maintainers\n",
		Desc:     "Initialize .github/CODEOWNERS",
		TaskPrio: int(p),
	})

	return plan, nil
}

// LicenseTask is a custom task for dynamic license generation.
type LicenseTask struct {
	TaskPrio int
}

// Description returns a human-readable summary of the task.
func (t *LicenseTask) Description() string { return "Generate LICENSE file" }

// Priority returns the execution priority level.
func (t *LicenseTask) Priority() int { return t.TaskPrio }

// Execute performs the license generation task.
//nolint:cyclop // Complex string replacement and file operations
func (t *LicenseTask) Execute(tc types.TaskContext) error {
	if tc.Manifest.Metadata == nil {
		return errors.New("missing compliance metadata (license and copyright_holder) in manifest")
	}

	licenseID := tc.Manifest.Metadata["license"]
	holders := tc.Manifest.Metadata["copyright_holder"]

	if licenseID == "" || holders == "" {
		return errors.New("explicit license and copyright_holder are required for the compliance template")
	}

	// 1. Fetch license text dynamically from SPDX.org
	lic, err := spdx.License(licenseID)
	if err != nil {
		return fmt.Errorf("failed to fetch license '%s': %w", licenseID, err)
	}

	text := lic.Text
	year := strconv.Itoa(time.Now().Year())

	// 2. Replace common SPDX placeholders
	// SPDX templates use various formats: <year>, [year], <copyright holders>, [fullname]
	replacer := strings.NewReplacer(
		"<year>", year,
		"[year]", year,
		"[yyyy]", year,
		"<copyright holders>", holders,
		"[fullname]", holders,
		"<name of author>", holders,
		"<owner>", holders,
		"[name of copyright owner]", holders,
	)
	text = replacer.Replace(text)

	// 3. Manual write with State-Aware Reconciliation
	if tc.DryRun {
		return nil
	}

	finalPath := filepath.Join(tc.TargetPath, "LICENSE")
	absPath, _ := filepath.Abs(finalPath)

	newHash := tasks.HashContent([]byte(text))

	// Re-implement reconciliation logic here to avoid circular dependency
	writePath := absPath
	if !tc.Force {
		//nolint:gosec
		existingContent, err := os.ReadFile(absPath)
		if err == nil {
			existingHash := tasks.HashContent(existingContent)
			var originalHash string
			if tc.Manifest.ManagedFiles != nil {
				originalHash = tc.Manifest.ManagedFiles["LICENSE"]
			}

			if originalHash == "" || (existingHash != originalHash && existingHash != newHash) {
				// Drift detected
				switch tc.ConflictStrategy {
				case "overwrite":
					writePath = absPath
				case "artifact":
					fmt.Printf("⚠️  Conflict in LICENSE (user modifications detected). Writing new template to artifact.\n")
					writePath = absPath + ".scbake-new"
				case "keep-local":
					fmt.Printf("⚠️  Conflict in LICENSE (user modifications detected). Skipping update (--strategy=keep-local).\n")
					return nil
				case "fail":
					fallthrough
				default:
					return errors.New("file LICENSE has manual modifications (drift detected). Use --conflict-strategy to resolve")
				}
			}
		}
	}

	if tc.Tx != nil {
		if err := tc.Tx.Track(writePath); err != nil {
			return fmt.Errorf("failed to track LICENSE file: %w", err)
		}
	}

	if err := os.WriteFile(writePath, []byte(text), fileutil.FilePerms); err != nil {
		return fmt.Errorf("failed to write LICENSE: %w", err)
	}

	// Record State
	if tc.Manifest.ManagedFiles == nil {
		tc.Manifest.ManagedFiles = make(map[string]string)
	}
	tc.Manifest.ManagedFiles["LICENSE"] = newHash

	return nil
}
