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
// It creates both the JSON file and the Dockerfile.
func (h *Handler) GetTasks(_ string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequence for the Dev Environment band (1500+)
	// Max is set to 0, indicating unlimited steps within this final band.
	seq := types.NewPrioritySequence(types.PrioDevEnv, 0)

	// Task 1: Create the lightweight, smart Dockerfile
	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "Dockerfile.tpl",
		OutputPath:   ".devcontainer/Dockerfile",
		Desc:         "Create .devcontainer/Dockerfile",
		TaskPrio:     int(p), // Now 1500
	})

	// Task 2: Create the devcontainer.json file
	p, err = seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "devcontainer.json.tpl",
		OutputPath:   ".devcontainer/devcontainer.json",
		Desc:         "Create .devcontainer/devcontainer.json",
		TaskPrio:     int(p), // Now 1501
	})

	return plan, nil
}
