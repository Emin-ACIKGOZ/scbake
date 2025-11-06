package makefile

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed makefile.tpl
var templates embed.FS

type Handler struct{}

// GetTasks signature updated, but logic is path-independent.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "makefile.tpl",
		OutputPath:   "Makefile",
		Desc:         "Create smart Makefile",
		TaskPrio:     400,
	})

	return plan, nil
}
