// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package golang provides the task handler for initializing Go projects.
package golang

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
)

//go:embed main.go.tpl gitignore.tpl
var templates embed.FS

// Handler implements the lang.Handler interface for Go projects.
type Handler struct{}

// GetTasks uses the sanitize utility to sanitize the module name and returns the execution plan.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Use a single sequence for all language setup tasks (100-999)
	langSeq := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)

	// Task 1: Create .gitignore
	p, err := langSeq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "gitignore.tpl",
		OutputPath:   fileutil.GitIgnore,
		Desc:         "Create " + fileutil.GitIgnore,
		TaskPrio:     int(p),
	})

	// Task 2: Create main.go
	p, err = langSeq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "main.go.tpl",
		OutputPath:   "main.go",
		Desc:         "Create main.go",
		TaskPrio:     int(p),
	})

	// Idempotency Check
	goModPath := filepath.Join(targetPath, "go.mod")
	_, checkErr := os.Stat(goModPath)

	if os.IsNotExist(checkErr) {
		// --- Path 1: go.mod does NOT exist (Initialization) ---
		moduleName, err := util.SanitizeModuleName(targetPath)
		if err != nil {
			return nil, fmt.Errorf("could not determine module name: %w", err)
		}

		// Task 3: Run 'go mod init'
		p, err = langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "go",
			Args:        []string{"mod", "init", moduleName}, // Use sanitized name
			Desc:        "Run go mod init " + moduleName,
			TaskPrio:    int(p),
			RunInTarget: true,
		})

		// Task 4: Run 'go mod tidy'
		p, err = langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "go",
			Args:        []string{"mod", "tidy"},
			Desc:        "Run go mod tidy",
			TaskPrio:    int(p),
			RunInTarget: true,
		})
	} else if checkErr == nil {
		// --- Path 2: go.mod *does* exist (Maintenance) ---
		// We only run 'go mod tidy' to update dependencies if files were modified.
		p, err := langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "go",
			Args:        []string{"mod", "tidy"},
			Desc:        "Run go mod tidy (project exists)",
			TaskPrio:    int(p),
			RunInTarget: true,
		})
	} else {
		// --- Path 3: Some other error ---
		return nil, fmt.Errorf("could not check for go.mod: %w", checkErr)
	}

	return plan, nil
}
