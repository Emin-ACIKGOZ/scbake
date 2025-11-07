package svelte

import (
	"fmt"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

type Handler struct{}

// GetTasks returns the list of tasks required to set up a Svelte project.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Task 1: Run 'npm create vite@latest <targetPath>'
	// We run this from the repository root (RunInTarget: false) and let
	// Vite handle creating the directory if it doesn't exist.
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd: "npm",
		Args: []string{
			"create",
			"vite@latest",
			targetPath, // e.g., "./frontend" or "."
			"--",       // Bypasses prompts
			"--template",
			"svelte",
		},
		Desc:        fmt.Sprintf("Run npm create vite@latest %s", targetPath),
		TaskPrio:    200,
		RunInTarget: false, // CHANGED: Run from root
	})

	// Task 2: Run 'npm install'
	// This must run *inside* the newly created project directory.
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "npm",
		Args:        []string{"install"},
		Desc:        "Run npm install",
		TaskPrio:    300,
		RunInTarget: true, // Runs IN the targetPath
	})

	return plan, nil
}
