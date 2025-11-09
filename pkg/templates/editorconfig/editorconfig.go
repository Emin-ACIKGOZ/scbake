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

	// Initialize sequence for the Universal Config band (1000-1099)
	seq := types.NewPrioritySequence(types.PrioConfigUniversal, types.MaxConfigUniversal)

	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: ".editorconfig.tpl",
		OutputPath:   ".editorconfig",
		Desc:         "Create standardized .editorconfig",
		TaskPrio:     int(p), // Now 1000
	})

	return plan, nil
}
