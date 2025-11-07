package devcontainer

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed devcontainer.json.tpl Dockerfile.tpl
var templates embed.FS

type Handler struct{}

// GetTasks returns the plan to create the Dev Container configuration.
// It creates both the JSON file and the Dockerfile.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Task 1: Create the supporting Dockerfile
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "Dockerfile.tpl",
		OutputPath:   ".devcontainer/Dockerfile",
		Desc:         "Create .devcontainer/Dockerfile",
		TaskPrio:     40,
	})

	// Task 2: Create the main devcontainer.json file
	// Since no TemplateData is explicitly provided, the *full manifest* is passed
	// as context, allowing the JSON template to use conditional logic.
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "devcontainer.json.tpl",
		OutputPath:   ".devcontainer/devcontainer.json",
		Desc:         "Create .devcontainer/devcontainer.json",
		TaskPrio:     41,
	})

	return plan, nil
}
