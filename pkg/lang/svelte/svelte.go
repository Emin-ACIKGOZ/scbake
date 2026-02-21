// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package svelte provides the task handler for initializing Svelte projects.
package svelte

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// Handler implements the lang.Handler interface for Svelte projects.
type Handler struct{}

// GetTasks returns the list of tasks required to set up a Svelte project.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequences
	dirSeq := types.NewPrioritySequence(types.PrioDirCreate, types.MaxDirCreate)
	langSeq := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)

	// Task 0: Ensure target directory exists.
	// This is critical because subsequent tasks use RunInTarget: true,
	// which requires the directory to exist beforehand.
	p, err := dirSeq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateDirTask{
		Path:     targetPath,
		Desc:     fmt.Sprintf("Create project directory '%s'", targetPath),
		TaskPrio: int(p), // Now 50
	})

	packageJSONPath := filepath.Join(targetPath, "package.json")
	_, checkErr := os.Stat(packageJSONPath)

	if os.IsNotExist(checkErr) {
		// Task 1: Run 'npm create vite@latest .' inside the target directory.
		// This initializes the project into the existing (empty) directory.
		p, err := langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd: "npm",
			Args: []string{
				"create",
				"vite@latest",
				".",  // Use '.' to create project in the CWD
				"--", // Bypasses prompts
				"--template",
				"svelte",
			},
			Desc:        "Run npm create vite@latest .",
			TaskPrio:    int(p), // Now 100
			RunInTarget: true,   // Runs inside targetPath
		})

		// Task 2: Run 'npm install'
		p, err = langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "npm",
			Args:        []string{"install"},
			Desc:        "Run npm install",
			TaskPrio:    int(p), // Now 101
			RunInTarget: true,
		})

		// Task 3: Explicitly set standard NPM scripts.
		// This guarantees 'npm run build' works for the Makefile, even if the
		// upstream template changes its default script names.
		p, err = langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd: "npm",
			Args: []string{
				"pkg",
				"set",
				"scripts.dev=vite",
				"scripts.build=vite build",
				"scripts.preview=vite preview",
				"scripts.check=svelte-check --tsconfig ./tsconfig.json",
			},
			Desc:        "Ensure standard NPM scripts are set",
			TaskPrio:    int(p), // Now 102
			RunInTarget: true,
		})
	} else if checkErr != nil {
		return nil, fmt.Errorf("failed to check for existing Svelte project: %w", checkErr)
	}

	return plan, nil
}
