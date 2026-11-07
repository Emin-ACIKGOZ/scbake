// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Package compliance provides templates for common security and compliance files.
package compliance

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"strconv"
	"strings"
	"time"

	"github.com/mitchellh/go-spdx"
)

//go:embed templates/*.tpl schema.json
var templates embed.FS

const licenseInputStateKey = "compliance.LICENSE"

var fetchSPDXLicense = spdx.License

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

// ManagedWrite implements types.ManagedWriter.
func (t *LicenseTask) ManagedWrite() {}

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

	year := time.Now().Year()
	inputHash := licenseInputHash(licenseID, holders, year)
	if !tc.Force && tc.Manifest.ManagedInputs[licenseInputStateKey] == inputHash && licenseFileIsClean(tc) {
		return nil
	}

	// 1. Fetch license text dynamically from SPDX.org
	lic, err := fetchSPDXLicense(licenseID)
	if err != nil {
		return fmt.Errorf("failed to fetch license '%s': %w", licenseID, err)
	}

	text := lic.Text
	// 2. Replace common SPDX placeholders.
	text = fillLicensePlaceholders(text, strconv.Itoa(year), holders)

	// 3. Write license file with state-aware reconciliation and transaction tracking
	if err := tasks.WriteManagedFile(tc, "LICENSE", []byte(text), fileutil.FilePerms); err != nil {
		return err
	}
	if tc.Manifest.ManagedInputs == nil {
		tc.Manifest.ManagedInputs = make(map[string]string)
	}
	tc.Manifest.ManagedInputs[licenseInputStateKey] = inputHash
	return nil
}

func licenseInputHash(licenseID, holders string, year int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", licenseID, holders, year)))
	return hex.EncodeToString(sum[:])
}

func licenseFileIsClean(tc types.TaskContext) bool {
	if tc.Manifest == nil || tc.Manifest.ManagedFiles == nil {
		return false
	}
	key := "LICENSE"
	if tc.Tx != nil && tc.Tx.Root() != "" {
		absTarget, err := filepath.Abs(tc.TargetPath)
		if err != nil {
			return false
		}
		rel, err := filepath.Rel(tc.Tx.Root(), filepath.Join(absTarget, "LICENSE"))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
		key = filepath.ToSlash(rel)
	}
	recordedHash := tc.Manifest.ManagedFiles[key]
	if recordedHash == "" {
		return false
	}
	content, err := fileutil.SafeReadFile(tc.TargetPath, "LICENSE")
	if err != nil {
		return false
	}
	return tasks.HashContent(content) == recordedHash
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
