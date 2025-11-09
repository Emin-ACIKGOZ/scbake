package mavenlinter

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed checkstyle.xml.tpl pom_snippet.xml.tpl
var templates embed.FS

type Handler struct{}

// GetTasks returns the plan to create Checkstyle config and update pom.xml.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Task 1: Create the Checkstyle config file
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "checkstyle.xml.tpl",
		OutputPath:   "checkstyle.xml",
		Desc:         "Create Maven Checkstyle configuration",
		TaskPrio:     1030,
	})

	// Task 2: Create the placeholder for the Checkstyle plugin snippet
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "pom_snippet.xml.tpl",
		OutputPath:   "maven-checkstyle-plugin.xml",
		Desc:         "Create Maven pom.xml snippet (Checkstyle)",
		TaskPrio:     1031,
	})

	return plan, nil
}
