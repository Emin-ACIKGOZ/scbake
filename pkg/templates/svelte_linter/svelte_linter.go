// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package sveltelinter

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed eslint.config.js.tpl
var templates embed.FS

// Handler implements the templates.Handler interface for Svelte linting.
type Handler struct{}

// GetTasks returns the plan to add ESLint configuration and dependencies for Svelte.
func (h *Handler) GetTasks(_ string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequence for the Linter band (1200-1399)
	seq := types.NewPrioritySequence(types.PrioLinter, types.MaxLinter)

	// Task 1: Create the ESLint config file (eslint.config.js)
	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "eslint.config.js.tpl",
		OutputPath:   "eslint.config.js",
		Desc:         "Create Svelte ESLint 9 configuration",
		TaskPrio:     int(p), // Now 1200
	})

	// Task 2: Install necessary ESLint dependencies
	p, err = seq.Next()
	if err != nil {
		return nil, err
	}
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
		TaskPrio:    int(p), // Now 1201
		RunInTarget: true,
	})

	// Task 3: Add robust 'lint' and 'lint:fix' scripts to package.json
	p, err = seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd: "npm",
		Args: []string{
			"pkg",
			"set",
			"scripts.lint=npx eslint .",
			"scripts.lint:fix=npx eslint . --fix",
		},
		Desc:        "Add standard lint scripts to package.json",
		TaskPrio:    int(p), // Now 1202
		RunInTarget: true,
	})

	return plan, nil
}
