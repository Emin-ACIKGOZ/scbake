package makefile

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed makefile.tpl
var templates embed.FS

// Handler implements the logic for scaffolding a Makefile.
type Handler struct{}

// GetTasks returns the list of tasks required to set up a Makefile.
func (h *Handler) GetTasks() ([]types.Task, error) {
	var plan []types.Task

	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "makefile.tpl",
		OutputPath:   "Makefile",
		Desc:         "Create smart Makefile",
		TaskPrio:     400, // Synthesis tasks run late
	})

	return plan, nil
}
