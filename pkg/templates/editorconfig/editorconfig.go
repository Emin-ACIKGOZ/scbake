package editorconfig

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed .editorconfig.tpl
var templates embed.FS

type Handler struct{}

// GetTasks returns the plan to create the standard .editorconfig file.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: ".editorconfig.tpl",
		OutputPath:   ".editorconfig",
		Desc:         "Create standardized .editorconfig",
		TaskPrio:     1010, // Run early, universal config
	})

	return plan, nil
}
