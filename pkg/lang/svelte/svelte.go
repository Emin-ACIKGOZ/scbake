// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package svelte

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

const (
	// npmCmd is the Node package manager executable invoked for setup tasks.
	npmCmd = "npm"

	packageJSONFile = "package.json"
	packageLockFile = "package-lock.json"
	nodeModulesDir  = "node_modules"
)

// Handler implements lang.Handler.
type Handler struct{}

// GetTasks returns the list of tasks required to set up a Svelte project.
func (h *Handler) GetTasks(targetPath string, _ string, _ string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequences
	dirSeq, err := types.NewPrioritySequence(types.PrioDirCreate, types.MaxDirCreate)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}
	langSeq, err := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

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

	packageJSONPath := filepath.Join(targetPath, packageJSONFile)
	_, checkErr := os.Stat(packageJSONPath)

	if os.IsNotExist(checkErr) {
		// Task 1: Run 'npm create vite@latest .' in an empty staging leaf and
		// move the output into the target. create-vite cancels when the target
		// is non-empty; for `new`, the target already holds scbake.toml and
		// .scbake.
		p, err := langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ScaffoldTask{
			Cmd: npmCmd,
			Args: []string{
				"create",
				"vite@latest",
				".",  // Use '.' to create project in the CWD
				"--", // Bypasses prompts
				"--template",
				"svelte",
			},
			Desc:     "Run npm create vite@latest . (staged)",
			TaskPrio: int(p), // Now 100
			// create-vite cancels with exit 0 and writes nothing, so
			// package.json proves it produced a project.
			Sentinel: packageJSONFile,
		})

		// Task 2: Run 'npm install'
		p, err = langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:              npmCmd,
			Args:             []string{"install"},
			Desc:             "Run npm install",
			TaskPrio:         int(p), // Now 101
			PredictedCreated: []string{packageJSONFile, packageLockFile, nodeModulesDir},
			RunInTarget:      true,
		})

		// Task 3: Explicitly set standard NPM scripts.
		// This guarantees 'npm run build' works for the Makefile, even if the
		// upstream template changes its default script names.
		p, err = langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd: npmCmd,
			Args: []string{
				"pkg",
				"set",
				"scripts.dev=vite",
				"scripts.build=vite build",
				"scripts.preview=vite preview",
				"scripts.check=svelte-check --tsconfig ./tsconfig.json",
			},
			Desc:             "Ensure standard NPM scripts are set",
			TaskPrio:         int(p), // Now 102
			PredictedCreated: []string{packageJSONFile},
			RunInTarget:      true,
		})
	} else if checkErr != nil {
		return nil, fmt.Errorf("failed to check for existing Svelte project: %w", checkErr)
	}

	return plan, nil
}
