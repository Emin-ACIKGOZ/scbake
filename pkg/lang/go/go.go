package golang

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util"
	"scbake/pkg/tasks"
)

//go:embed main.go.tpl gitignore.tpl
var templates embed.FS

type Handler struct{}

// GetTasks uses the sanitize utility to sanitize the module name.
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

	// Idempotency Check
	goModPath := filepath.Join(targetPath, "go.mod")
	_, err := os.Stat(goModPath)

	if os.IsNotExist(err) {
		// --- Path 1: go.mod does NOT exist (Initialization) ---
		moduleName, err := util.SanitizeModuleName(targetPath)
		if err != nil {
			return nil, fmt.Errorf("could not determine module name: %w", err)
		}

		// Task 3: Run 'go mod init'
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "go",
			Args:        []string{"mod", "init", moduleName}, // Use sanitized name
			Desc:        "Run go mod init " + moduleName,
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
		// --- Path 2: go.mod *does* exist (Maintenance) ---
		// We only run 'go mod tidy' to update dependencies if files were modified.
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "go",
			Args:        []string{"mod", "tidy"},
			Desc:        "Run go mod tidy (project exists)",
			TaskPrio:    300,
			RunInTarget: true,
		})
	} else {
		// --- Path 3: Some other error ---
		return nil, fmt.Errorf("could not check for go.mod: %w", err)
	}

	return plan, nil
}
