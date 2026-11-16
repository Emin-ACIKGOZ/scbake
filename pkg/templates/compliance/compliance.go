// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Package compliance provides templates for common security and compliance files.
package compliance

import (
	"embed"
	"errors"
	"fmt"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"scbake/pkg/templates/compliance/internal/spdx"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/*.tpl schema.json
var templates embed.FS

// Handler implements templates.Handler.
type Handler struct{}

// SchemaFS implements templates.SchemaProvider.
func (h *Handler) SchemaFS() embed.FS { return templates }

// SchemaPath implements templates.SchemaProvider.
func (h *Handler) SchemaPath() string { return "schema.json" }

// GetTasks returns the plan to create compliance files.
func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
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

// Description implements types.Task.
func (t *LicenseTask) Description() string { return "Generate LICENSE file" }

// Priority implements types.Task.
func (t *LicenseTask) Priority() int { return t.TaskPrio }

// WritesManagedFile marks this task as safe to replay during update/repair: it
// reconciles LICENSE, which is recorded in the managed_files ledger.
func (t *LicenseTask) WritesManagedFile() bool { return true }

// Compile-time guard: update/repair detect managed writers by this method.
var _ interface{ WritesManagedFile() bool } = (*LicenseTask)(nil)

// Execute performs the license generation task.
func (t *LicenseTask) Execute(tc types.TaskContext) error {
	if tc.Manifest.Metadata == nil {
		return errors.New("missing compliance metadata (license and copyright_holder) in manifest")
	}

	licenseID := tc.Manifest.Metadata["license"]
	holders := tc.Manifest.Metadata["copyright_holder"]

	if licenseID == "" || holders == "" {
		return errors.New("explicit license and copyright_holder are required for the compliance template")
	}

	// During update/repair, a clean managed LICENSE is already reconciled. Skip
	// loading the text from the embedded snapshot so update does not re-validate
	// the recorded license ID and the copyright year is not silently re-stamped.
	// `apply` still renders unconditionally, so changing license terms is done
	// with `apply --with compliance`.
	if tc.Update {
		unchanged, err := tasks.ManagedFileUnchanged(tc, "LICENSE")
		if err != nil {
			return err
		}
		if unchanged {
			// Repair a missing or stale pristine base from the current bytes
			// without loading the license text again.
			if _, err := tasks.EnsureManagedFileBase(tc, "LICENSE"); err != nil {
				return err
			}
			return nil
		}
	}

	// 1. Look up the license text in the embedded SPDX snapshot.
	text, err := spdx.Text(licenseID)
	if err != nil {
		return fmt.Errorf("failed to load license %q: %w", licenseID, err)
	}

	year := strconv.Itoa(time.Now().Year())

	// 2. Replace common SPDX placeholders.
	text = fillLicensePlaceholders(text, year, holders)

	// 3. Write license file with state-aware reconciliation and transaction tracking
	return tasks.WriteManagedFile(tc, "LICENSE", []byte(text), fileutil.FilePerms)
}

// fillLicensePlaceholders replaces the year and copyright-holder fields used by
// SPDX license texts. Matching is literal; SPDX casing is inconsistent.
func fillLicensePlaceholders(text, year, holders string) string {
	return strings.NewReplacer(
		"<year>", year,
		"<YEAR>", year,
		"<Year>", year,
		"[year]", year,
		"[YEAR]", year,
		"[Year]", year,
		"[yyyy]", year,
		"<copyright holder>", holders,
		"<COPYRIGHT HOLDER>", holders,
		"<copyright holders>", holders,
		"<COPYRIGHT HOLDERS>", holders,
		"[copyright holders]", holders,
		"[name of copyright holder]", holders,
		"[name of copyright owner]", holders,
		"[fullname]", holders,
		"<name of author>", holders,
		"<owner>", holders,
	).Replace(text)
}
