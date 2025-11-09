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

	// Initialize sequence for the Build System band (1400-1499)
	seq := types.NewPrioritySequence(types.PrioBuildSystem, types.MaxBuildSystem)

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
