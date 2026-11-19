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

	// dryRunNote is the read-only reconciliation note recorded by Validate and
	// surfaced through DryRunNote. It stays empty for an ordinary write and
	// names the update skip path's only real effect: "base repair only" when the
	// pristine base shadow needs backfilling, or "no change" when it is clean.
	dryRunNote string
}

// Description implements types.Task.
func (t *LicenseTask) Description() string { return "Generate LICENSE file" }

// Priority implements types.Task.
func (t *LicenseTask) Priority() int { return t.TaskPrio }

// WritesManagedFile marks this task as safe to replay during update/repair: it
// reconciles LICENSE, which is recorded in the managed_files ledger.
func (t *LicenseTask) WritesManagedFile() bool { return true }

// ManagedOutputPath returns the target-relative path this task writes. The plan
// filter joins it with the project's root-relative path to compare against
// unmanaged_files before any task runs.
func (t *LicenseTask) ManagedOutputPath() string { return "LICENSE" }

// Compile-time guard: update/repair detect managed writers by this method.
var _ interface{ WritesManagedFile() bool } = (*LicenseTask)(nil)

// licenseRender validates the compliance metadata and resolves the license
// text. skip is true when an update's LICENSE is already current; the apply
// path then only repairs the pristine base shadow. It is read-only and shared
// by Validate and Execute so the preview reports the real decision.
func licenseRender(tc types.TaskContext) (text string, skip bool, err error) {
	if tc.Manifest.Metadata == nil {
		return "", false, errors.New("missing compliance metadata (license and copyright_holder) in manifest")
	}

	licenseID := tc.Manifest.Metadata["license"]
	holders := tc.Manifest.Metadata["copyright_holder"]

	if licenseID == "" || holders == "" {
		return "", false, errors.New("explicit license and copyright_holder are required for the compliance template")
	}

	// During update/repair, a clean managed LICENSE is already reconciled. Skip
	// loading the text from the embedded snapshot so update does not re-validate
	// the recorded license ID and the copyright year is not silently re-stamped.
	// `apply` still renders unconditionally, so changing license terms is done
	// with `apply --with compliance`.
	if tc.Update {
		unchanged, err := tasks.ManagedFileUnchanged(tc, "LICENSE")
		if err != nil {
			return "", false, err
		}
		if unchanged {
			return "", true, nil
		}
	}

	// 1. Look up the license text in the embedded SPDX snapshot.
	text, err = spdx.Text(licenseID)
	if err != nil {
		return "", false, fmt.Errorf("failed to load license %q: %w", licenseID, err)
	}

	year := strconv.Itoa(time.Now().Year())

	// 2. Replace common SPDX placeholders.
	return fillLicensePlaceholders(text, year, holders), false, nil
}

// DryRunNote implements types.DryRunAnnotator. It is empty for an ordinary
// license write and annotates the update skip path as a base repair or no-op.
func (t *LicenseTask) DryRunNote() string { return t.dryRunNote }

// DryRunVerb implements types.DryRunVerbProvider so the preview says "Skip"
// instead of "Generate" when the reconciliation is a no-op, a base-only repair,
// or preserves local edits, and names the artifact when a conflict diverts the
// write.
func (t *LicenseTask) DryRunVerb() string { return tasks.DryRunVerbForNote(t.dryRunNote) }

// Validate checks compliance metadata, license resolution, and the managed-file
// reconciliation decision without writing. It shares licenseRender and
// tasks.PlanManagedFile with Execute, so an unmanaged or drifted LICENSE fails
// in a dry run exactly as it does in a real run and predicts the same planned
// write.
func (t *LicenseTask) Validate(tc types.TaskContext) error {
	t.dryRunNote = ""

	text, skip, err := licenseRender(tc)
	if err != nil {
		return err
	}
	if skip {
		// The update path only repairs the pristine base shadow from the
		// current bytes; that is a write and real-run-only. Plan with those
		// bytes so overwrite still predicts the canonical write (and Merge
		// stays conservative), then annotate the actual effect: a base repair
		// when the shadow is missing or stale, and no change when it is clean.
		existing, err := fileutil.SafeReadFile(tc.TargetPath, "LICENSE")
		if err != nil {
			return err
		}
		if _, err = tasks.PlanManagedFile(tc, "LICENSE", existing); err != nil {
			return err
		}
		needsRepair, err := tasks.ManagedFileBaseNeedsRepair(tc, "LICENSE")
		if err != nil {
			return err
		}
		if needsRepair {
			t.dryRunNote = "base repair only"
		} else {
			t.dryRunNote = "no change"
		}
		return nil
	}

	t.dryRunNote, err = tasks.PlanManagedFile(tc, "LICENSE", []byte(text))
	return err
}

// Execute performs the license generation task.
func (t *LicenseTask) Execute(tc types.TaskContext) error {
	text, skip, err := licenseRender(tc)
	if err != nil {
		return err
	}
	if skip {
		// Repair a missing or stale pristine base from the current bytes
		// without loading the license text again.
		if _, err := tasks.EnsureManagedFileBase(tc, "LICENSE"); err != nil {
			return err
		}
		return nil
	}

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
