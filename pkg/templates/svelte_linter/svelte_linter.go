package sveltelinter

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed .eslintrc.js.tpl
var templates embed.FS

type Handler struct{}

// GetTasks returns the plan to add ESLint configuration and dependencies for Svelte.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Task 1: Create the ESLint config file (.eslintrc.js)
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: ".eslintrc.js.tpl",
		OutputPath:   ".eslintrc.js",
		Desc:         "Create Svelte ESLint configuration",
		TaskPrio:     30,
	})

	// Task 2: Install necessary ESLint dependencies into the project
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd: "npm",
		Args: []string{
			"install",
			"--save-dev",
			"eslint",
			"@sveltejs/eslint-config",
			"prettier",
		},
		Desc:        "Install Svelte ESLint dependencies",
		TaskPrio:    31,
		RunInTarget: true, // Must run inside the project directory
	})

	return plan, nil
}
