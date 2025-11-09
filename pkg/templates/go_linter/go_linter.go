package golinter

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed .golangci.yml.tpl
var templates embed.FS

type Handler struct{}

// GetTasks returns the plan to create the Go linter configuration file.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: ".golangci.yml.tpl",
		OutputPath:   ".golangci.yml",
		Desc:         "Create Go linter configuration (.golangci.yml)",
		TaskPrio:     1015, // Same priority as universal editorconfig
	})

	return plan, nil
}
