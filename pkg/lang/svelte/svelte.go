package svelte

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

type Handler struct{}

// GetTasks returns the list of tasks required to set up a Svelte project.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Task 0: Ensure target directory exists.
	// This is critical because subsequent tasks use RunInTarget: true,
	// which requires the directory to exist beforehand.
	plan = append(plan, &tasks.CreateDirTask{
		Path:     targetPath,
		Desc:     fmt.Sprintf("Create project directory '%s'", targetPath),
		TaskPrio: 50, // Must run before npm commands (prio 200+)
	})

	packageJSONPath := filepath.Join(targetPath, "package.json")
	_, err := os.Stat(packageJSONPath)

	if os.IsNotExist(err) {
		// Task 1: Run 'npm create vite@latest .' inside the target directory.
		// This initializes the project into the existing (empty) directory.
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
			TaskPrio:    200,
			RunInTarget: true, // Runs inside targetPath
		})

		// Task 2: Run 'npm install' to fetch dependencies.
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "npm",
			Args:        []string{"install"},
			Desc:        "Run npm install",
			TaskPrio:    300,
			RunInTarget: true,
		})

		// Task 3: Explicitly set standard NPM scripts.
		// This guarantees 'npm run build' works for the Makefile, even if the
		// upstream template changes its default script names.
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
			TaskPrio:    301,
			RunInTarget: true,
		})

	} else if err != nil {
		return nil, fmt.Errorf("failed to check for existing Svelte project: %w", err)
	}

	return plan, nil
}
