// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Package community provides templates for open-source community standards.
package community

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed templates/*.tpl
var templates embed.FS

// Handler implements templates.Handler.
type Handler struct{}

// GetTasks returns the plan to create community governance files.
func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	return tasks.BuildTemplateTasks(templates, types.PrioConfigUniversal, types.MaxConfigUniversal,
		tasks.TemplateFileSpec{TemplatePath: "templates/CONTRIBUTING.md.tpl", OutputPath: "CONTRIBUTING.md", Desc: "Create CONTRIBUTING.md"},
		tasks.TemplateFileSpec{TemplatePath: "templates/CODE_OF_CONDUCT.md.tpl", OutputPath: "CODE_OF_CONDUCT.md", Desc: "Create CODE_OF_CONDUCT.md"},
		tasks.TemplateFileSpec{TemplatePath: "templates/SUPPORT.md.tpl", OutputPath: "SUPPORT.md", Desc: "Create SUPPORT.md"},
		tasks.TemplateFileSpec{TemplatePath: "templates/GOVERNANCE.md.tpl", OutputPath: "GOVERNANCE.md", Desc: "Create GOVERNANCE.md"},
	)
}
