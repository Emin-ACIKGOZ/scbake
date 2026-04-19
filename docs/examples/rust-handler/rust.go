// Package rust provides a Rust language handler for scbake.
// This is an example of how to extend scbake with a custom language.
//
// To use this handler:
// 1. Copy this file to pkg/lang/rust/rust.go
// 2. Create templates directory: pkg/lang/rust/templates/
// 3. Add templates: Cargo.toml.tpl, main.rs.tpl (see examples below)
// 4. Register in pkg/lang/registry.go: Register("rust", &rust.Handler{})
// 5. Build: go build -o scbake main.go
// 6. Use: scbake new my-app --lang rust
//
package rust

import (
	"embed"
	"fmt"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed templates/*
var templates embed.FS

// Handler implements the language handler interface for Rust projects.
type Handler struct{}

// GetTasks returns the list of tasks to set up a Rust project.
func (h *Handler) GetTasks(_ string) ([]types.Task, error) {
	// Create a priority sequence for language setup tasks
	seq, err := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	var plan []types.Task

	// Task 1: Create src directory
	p, _ := seq.Next()
	plan = append(plan, &tasks.CreateDirTask{
		Path:     "src",
		TaskPrio: int(p),
		Desc:     "Create src directory",
	})

	// Task 2: Create .gitignore for Rust projects
	p, _ = seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplatePath: string(mustRead("templates/gitignore.tpl")),
		OutputPath:   ".gitignore",
		TaskPrio:     int(p),
		Desc:         "Create .gitignore",
	})

	// Task 3: Create Cargo.toml from template
	p, _ = seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplatePath: string(mustRead("templates/Cargo.toml.tpl")),
		OutputPath:   "Cargo.toml",
		TaskPrio:     int(p),
		Desc:         "Create Cargo.toml",
	})

	// Task 4: Create main.rs
	p, _ = seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplatePath: string(mustRead("templates/main.rs.tpl")),
		OutputPath:   "src/main.rs",
		TaskPrio:     int(p),
		Desc:         "Create main.rs",
	})

	return plan, nil
}

// mustRead reads a template file and panics if it fails.
// In production, you'd handle this error more gracefully.
func mustRead(path string) []byte {
	data, err := templates.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("failed to read template %s: %v", path, err))
	}
	return data
}
