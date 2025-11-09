package cighub

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed main.yml.tpl
var templates embed.FS

type Handler struct{}

// GetTasks returns the plan to create the GitHub Actions workflow file.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// This task creates the workflow file in the required GitHub location.
	// Since no TemplateData is provided here, the template will receive the
	// *full manifest* as its execution context, allowing conditional logic.
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "main.yml.tpl",
		OutputPath:   ".github/workflows/main.yml",
		Desc:         "Create GitHub Actions CI workflow",
		TaskPrio:     1020, // Run early
	})

	return plan, nil
}
