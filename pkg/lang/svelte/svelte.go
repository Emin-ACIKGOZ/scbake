package svelte

import (
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

type Handler struct{}

// GetTasks returns the list of tasks required to set up a Svelte project.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// npm create vite expects just the name if creating in the current dir,
	// or a path if creating a sub-dir.
	// Since we want atomic rollback, we prefer letting Vite create the directory.
	// If targetPath is ".", we need a name.
	projectName := filepath.Base(targetPath)
	if projectName == "." || projectName == "/" {
		abs, _ := filepath.Abs(targetPath)
		projectName = filepath.Base(abs)
	}

	// Task 1: Run 'npm create vite@latest'
	// We run this in the *parent* of the target path so Vite can create the directory.
	// If targetPath is ".", it runs in ".." and creates current dir name.
	// This is a bit tricky with our current ExecCommandTask which only supports "." or targetPath.
	//
	// SIMPLIFICATION for v1: We assume the user wants to create the project IN the target path.
	// But Vite wants to CREATE the directory.
	//
	// Best approach for scbake standard: We let scbake create the dir (done in 'new' or by user before 'apply').
	// We then run vite inside it with '.' as the target name.

	plan = append(plan, &tasks.ExecCommandTask{
		Cmd: "npm",
		Args: []string{
			"create",
			"vite@latest",
			".",  // Create in current directory (targetPath)
			"--", // Bypasses prompts
			"--template",
			"svelte",
		},
		Desc:        "Run npm create vite@latest . -- --template svelte",
		TaskPrio:    200,
		RunInTarget: true, // Run INSIDE targetPath
	})

	// Task 2: Run 'npm install'
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "npm",
		Args:        []string{"install"},
		Desc:        "Run npm install",
		TaskPrio:    300,
		RunInTarget: true,
	})

	return plan, nil
}
