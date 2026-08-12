// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package cighub

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed main.yml.tpl schema.json
var templates embed.FS

// Handler implements the templates.Handler interface for GitHub CI.
type Handler struct{}

// SchemaFS returns the embedded filesystem containing schema.json.
func (h *Handler) SchemaFS() embed.FS { return templates }

// SchemaPath returns the path to the embedded schema definition.
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
