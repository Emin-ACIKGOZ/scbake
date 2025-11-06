package golang

import (
	"embed"
	"fmt"
	"os" // Import
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed main.go.tpl gitignore.tpl
var templates embed.FS

type Handler struct{}

// GetTasks now uses targetPath to create a dynamic module name.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Task 1: Create .gitignore
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "gitignore.tpl",
		OutputPath:   ".gitignore",
		Desc:         "Create .gitignore",
		TaskPrio:     100,
	})

	// Task 2: Create main.go
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "main.go.tpl",
		OutputPath:   "main.go",
		Desc:         "Create main.go",
		TaskPrio:     100,
	})

	// --- IDEMPOTENCY CHECK ---
	// Check if go.mod already exists.
	goModPath := filepath.Join(targetPath, "go.mod")
	_, err := os.Stat(goModPath)

	if err != nil && os.IsNotExist(err) {
		// --- Path 1: go.mod does NOT exist ---
		// We can safely run 'go mod init'.
		moduleName := filepath.Base(targetPath)
		if moduleName == "." || moduleName == "/" {
			abs, _ := filepath.Abs(targetPath)
			moduleName = filepath.Base(abs)
		}

		// Task 3: Run 'go mod init'
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "go",
			Args:        []string{"mod", "init", moduleName},
			Desc:        fmt.Sprintf("Run go mod init %s", moduleName),
			TaskPrio:    200,
			RunInTarget: true,
		})

		// Task 4: Run 'go mod tidy'
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "go",
			Args:        []string{"mod", "tidy"},
			Desc:        "Run go mod tidy",
			TaskPrio:    300,
			RunInTarget: true,
		})

	} else if err == nil {
		// --- Path 2: go.mod *does* exist ---
		// This is a re-run or a --force run.
		// We must NOT run 'go mod init'. We only run 'go mod tidy'.
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "go",
			Args:        []string{"mod", "tidy"},
			Desc:        "Run go mod tidy (project exists)",
			TaskPrio:    300,
			RunInTarget: true,
		})

	} else {
		// --- Path 3: Some other error ---
		// e.g., permissions error on os.Stat. We should fail.
		return nil, fmt.Errorf("could not check for go.mod: %w", err)
	}

	return plan, nil
}
