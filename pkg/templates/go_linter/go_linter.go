// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package golinter

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed .golangci.yml.tpl
var templates embed.FS

// Handler implements the templates.Handler interface for Go linting.
type Handler struct{}

// GetTasks returns the plan to create the Go linter configuration file.
func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	return tasks.BuildTemplateTasks(templates, types.PrioLinter, types.MaxLinter,
		tasks.TemplateFileSpec{
			TemplatePath: ".golangci.yml.tpl",
			OutputPath:   ".golangci.yml",
			Desc:         "Create Go linter configuration (.golangci.yml)",
		},
	)
}
