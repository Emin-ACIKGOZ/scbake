// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

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
func (h *Handler) GetTasks(_ string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequence for the Linter band (1200-1399)
	seq := types.NewPrioritySequence(types.PrioLinter, types.MaxLinter)

	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: ".golangci.yml.tpl",
		OutputPath:   ".golangci.yml",
		Desc:         "Create Go linter configuration (.golangci.yml)",
		TaskPrio:     int(p), // Now 1200
	})

	return plan, nil
}
