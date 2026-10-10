// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package editorconfig

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed .editorconfig.tpl
var templates embed.FS

// Handler implements the templates.Handler interface for EditorConfig.
type Handler struct{}

// GetTasks returns the plan to create the standard .editorconfig file.
func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	return tasks.BuildTemplateTasks(templates, types.PrioConfigUniversal, types.MaxConfigUniversal,
		tasks.TemplateFileSpec{
			TemplatePath: ".editorconfig.tpl",
			OutputPath:   ".editorconfig",
			Desc:         "Create standardized .editorconfig",
		},
	)
}
