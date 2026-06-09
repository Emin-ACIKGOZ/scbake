// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package makefile

import (
	"embed"
	"fmt"
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
	var plan []types.Task

	// Initialize sequence for the Build System band (1400-1499)
	seq, err := types.NewPrioritySequence(types.PrioBuildSystem, types.MaxBuildSystem)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "makefile.tpl",
		OutputPath:   "Makefile",
		Desc:         "Create smart Makefile",
		TaskPrio:     int(p), // Now 1400
	})

	return plan, nil
}
