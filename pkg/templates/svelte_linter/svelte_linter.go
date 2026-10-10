// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package sveltelinter

import (
	"embed"
	"fmt"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed eslint.config.js.tpl
var templates embed.FS

// Handler implements the templates.Handler interface for Svelte linting.
type Handler struct{}

// GetTasks returns the plan to add ESLint configuration and dependencies for Svelte.
func (h *Handler) GetTasks(_ string, _, _ string) ([]types.Task, error) {
	seq, err := types.NewPrioritySequence(types.PrioLinter, types.MaxLinter)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	p1, _ := seq.Next()
	p2, _ := seq.Next()
	p3, _ := seq.Next()

	var plan []types.Task
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "eslint.config.js.tpl",
		OutputPath:   "eslint.config.js",
		Desc:         "Create Svelte ESLint 9 configuration",
		TaskPrio:     int(p1),
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
		TaskPrio:    int(p2),
		RunInTarget: true,
	})

	// Task 3: Add robust 'lint' and 'lint:fix' scripts to package.json
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd: "npm",
		Args: []string{
			"pkg",
			"set",
			"scripts.lint=npx eslint .",
			"scripts.lint:fix=npx eslint . --fix",
		},
		Desc:        "Add standard lint scripts to package.json",
		TaskPrio:    int(p3),
		RunInTarget: true,
	})

	return plan, nil
}
