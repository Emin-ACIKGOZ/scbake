// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package makefile

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed makefile.tpl schema.json
var templates embed.FS

// Handler implements the templates.Handler interface for the Makefile.
type Handler struct{}

// SchemaFS implements templates.SchemaProvider.
func (h *Handler) SchemaFS() embed.FS { return templates }

// SchemaPath implements templates.SchemaProvider.
func (h *Handler) SchemaPath() string { return "schema.json" }

// GetTasks returns the plan to create the smart Makefile.
func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	return tasks.BuildTemplateTasks(templates, types.PrioBuildSystem, types.MaxBuildSystem,
		tasks.TemplateFileSpec{
			TemplatePath: "makefile.tpl",
			OutputPath:   "Makefile",
			Desc:         "Create smart Makefile",
		},
	)
}
