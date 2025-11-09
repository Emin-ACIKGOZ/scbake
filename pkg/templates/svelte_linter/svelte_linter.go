package sveltelinter

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed eslint.config.js.tpl
var templates embed.FS

type Handler struct{}

// GetTasks returns the plan to add ESLint configuration and dependencies for Svelte.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Task 1: Create the ESLint config file (eslint.config.js)
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "eslint.config.js.tpl",
		OutputPath:   "eslint.config.js",
		Desc:         "Create Svelte ESLint 9 configuration",
		TaskPrio:     1030,
	})

	// Task 2: Install necessary ESLint dependencies
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd: "npm",
		Args: []string{
			"install",
			"--save-dev",
			"eslint",
			"eslint-plugin-svelte",
			"globals",
			"@eslint/js",
			"prettier",
			"eslint-config-prettier",
		},
		Desc:        "Install Svelte ESLint dependencies",
		TaskPrio:    1031,
		RunInTarget: true,
	})

	// Task 3: Add robust 'lint' and 'lint:fix' scripts to package.json
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd: "npm",
		Args: []string{
			"pkg",
			"set",
			// Use 'npx' to guarantee finding the local binary in all shells
			"scripts.lint=npx eslint .",
			"scripts.lint:fix=npx eslint . --fix",
		},
		Desc:        "Add standard lint scripts to package.json",
		TaskPrio:    1032,
		RunInTarget: true,
	})

	return plan, nil
}
