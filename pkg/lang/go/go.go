package golang

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed main.go.tpl gitignore.tpl
var templates embed.FS

// Handler implements the logic for scaffolding a Go project.
type Handler struct{}

// GetTasks returns the list of tasks required to set up a Go project.
func (h *Handler) GetTasks() ([]types.Task, error) {
	var plan []types.Task

	// Task 1: Create .gitignore
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "gitignore.tpl",
		OutputPath:   ".gitignore",
		TemplateData: nil,
		Desc:         "Create .gitignore",
		TaskPrio:     100,
	})

	// Task 2: Create main.go
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "main.go.tpl",
		OutputPath:   "main.go",
		TemplateData: nil,
		Desc:         "Create main.go",
		TaskPrio:     100,
	})

	// Task 3: Run 'go mod init'
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "go",
		Args:        []string{"mod", "init", "my-project"}, // We'll make 'my-project' dynamic later
		Desc:        "Run go mod init",
		TaskPrio:    200, // Runs after files are created
		RunInTarget: true,
	})

	// Task 4: Run 'go mod tidy'
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "go",
		Args:        []string{"mod", "tidy"},
		Desc:        "Run go mod tidy",
		TaskPrio:    300, // Runs after mod init
		RunInTarget: true,
	})

	return plan, nil
}
