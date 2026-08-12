// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

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

// SchemaFS returns the embedded filesystem containing schema.json.
func (h *Handler) SchemaFS() embed.FS { return templates }

// SchemaPath returns the path to the embedded schema definition.
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
