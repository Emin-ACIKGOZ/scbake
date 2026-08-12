// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package devcontainer

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed devcontainer.json.tpl Dockerfile.tpl
var templates embed.FS

// Handler implements the templates.Handler interface for Dev Containers.
type Handler struct{}

// GetTasks returns the plan to create the Dev Container configuration.
func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	return tasks.BuildTemplateTasks(templates, types.PrioDevEnv, 0,
		tasks.TemplateFileSpec{
			TemplatePath: "Dockerfile.tpl",
			OutputPath:   ".devcontainer/Dockerfile",
			Desc:         "Create .devcontainer/Dockerfile",
		},
		tasks.TemplateFileSpec{
			TemplatePath: "devcontainer.json.tpl",
			OutputPath:   ".devcontainer/devcontainer.json",
			Desc:         "Create .devcontainer/devcontainer.json",
		},
	)
}
