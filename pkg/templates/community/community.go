// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package community provides templates for open-source community standards.
package community

import (
	"embed"
	"fmt"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed templates/*.tpl
var templates embed.FS

// Handler implements the templates.Handler interface for community governance.
type Handler struct{}

// GetTasks returns the plan to create community governance files.
func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequence for the Community/Governance band (1000-1099 range, reusing PrioConfigUniversal)
	seq, err := types.NewPrioritySequence(types.PrioConfigUniversal, types.MaxConfigUniversal)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	files := []struct {
		tpl  string
		dest string
		desc string
	}{
		{"templates/CONTRIBUTING.md.tpl", "CONTRIBUTING.md", "Create CONTRIBUTING.md"},
		{"templates/CODE_OF_CONDUCT.md.tpl", "CODE_OF_CONDUCT.md", "Create CODE_OF_CONDUCT.md"},
		{"templates/SUPPORT.md.tpl", "SUPPORT.md", "Create SUPPORT.md"},
		{"templates/GOVERNANCE.md.tpl", "GOVERNANCE.md", "Create GOVERNANCE.md"},
	}

	for _, f := range files {
		p, err := seq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.CreateTemplateTask{
			TemplateFS:   templates,
			TemplatePath: f.tpl,
			OutputPath:   f.dest,
			Desc:         f.desc,
			TaskPrio:     int(p),
		})
	}

	return plan, nil
}
