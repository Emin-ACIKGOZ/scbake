// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cighub

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed main.yml.tpl schema.json
var templates embed.FS

// Handler implements templates.Handler.
type Handler struct{}

// SchemaFS implements templates.SchemaProvider.
func (h *Handler) SchemaFS() embed.FS { return templates }

// SchemaPath implements templates.SchemaProvider.
func (h *Handler) SchemaPath() string { return "schema.json" }

// GetTasks returns the plan to create the GitHub Actions workflow file.
func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	return tasks.BuildTemplateTasks(templates, types.PrioCI, types.MaxCI,
		tasks.TemplateFileSpec{
			TemplatePath: "main.yml.tpl",
			OutputPath:   ".github/workflows/main.yml",
			Desc:         "Create GitHub Actions CI workflow",
		},
	)
}
