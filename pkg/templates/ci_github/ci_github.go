// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package cighub

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed main.yml.tpl
var templates embed.FS

// Handler implements the templates.Handler interface for GitHub CI.
type Handler struct{}

// GetTasks returns the plan to create the GitHub Actions workflow file.
func (h *Handler) GetTasks(_ string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequence for the CI band (1100-1199)
	seq := types.NewPrioritySequence(types.PrioCI, types.MaxCI)

	// This task creates the workflow file in the required GitHub location.
	// Since no TemplateData is provided here, the template will receive the
	// *full manifest* as its execution context, allowing conditional logic.
	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "main.yml.tpl",
		OutputPath:   ".github/workflows/main.yml",
		Desc:         "Create GitHub Actions CI workflow",
		TaskPrio:     int(p), // Now 1100
	})

	return plan, nil
}
