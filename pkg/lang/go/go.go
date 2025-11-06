package golang

import (
	"embed"
	"fmt"           // Import
	"path/filepath" // Import
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed main.go.tpl gitignore.tpl
var templates embed.FS

type Handler struct{}

// GetTasks now uses targetPath to create a dynamic module name.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Task 1: .gitignore (unchanged)
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "gitignore.tpl",
		OutputPath:   ".gitignore",
		Desc:         "Create .gitignore",
		TaskPrio:     100,
	})

	// Task 2: main.go (unchanged)
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "main.go.tpl",
		OutputPath:   "main.go",
		Desc:         "Create main.go",
		TaskPrio:     100,
	})

	// --- THIS IS THE CHANGE ---
	// Use the base of the target path as the module name
	// e.g., "./backend" -> "backend"
	// e.g., "." -> "scbake" (or the parent dir name)
	moduleName := filepath.Base(targetPath)
	if moduleName == "." || moduleName == "/" {
		// If target is ".", use the parent dir name
		abs, _ := filepath.Abs(targetPath)
		moduleName = filepath.Base(abs)
	}
	// --- END CHANGE ---

	// Task 3: Run 'go mod init'
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "go",
		Args:        []string{"mod", "init", moduleName}, // Use dynamic moduleName
		Desc:        fmt.Sprintf("Run go mod init %s", moduleName),
		TaskPrio:    200,
		RunInTarget: true,
	})

	// Task 4: Run 'go mod tidy' (unchanged)
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "go",
		Args:        []string{"mod", "tidy"},
		Desc:        "Run go mod tidy",
		TaskPrio:    300,
		RunInTarget: true,
	})

	return plan, nil
}
