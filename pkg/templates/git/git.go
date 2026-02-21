// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package git provides the template handler for initializing a Git repository.
package git

import (
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// Handler implements the templates.Handler interface for Git.
type Handler struct{}

// GetTasks returns the plan to initialize a Git repository.
// It performs `git init`, `git add .`, and `git commit`.
func (h *Handler) GetTasks(_ string) ([]types.Task, error) {
	var plan []types.Task

	// Use PrioritySequence to avoid magic numbers and ensure strictly ordered execution
	// within the VersionControl band.
	seq := types.NewPrioritySequence(types.PrioVersionControl, types.MaxVersionControl)

	// Task 1: Initialize Git repository.
	// We predict creation of ".git" so the transaction system can rollback the entire repo creation on failure.
	prio, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:              "git",
		Args:             []string{"init"},
		Desc:             "Initialize Git repository",
		TaskPrio:         int(prio),
		RunInTarget:      true,
		PredictedCreated: []string{".git"},
	})

	// Task 2: Stage all files.
	prio, err = seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "git",
		Args:        []string{"add", "."},
		Desc:        "Stage files",
		TaskPrio:    int(prio),
		RunInTarget: true,
	})

	// Task 3: Create initial commit.
	// We use "commit -m ..." to snapshot the scaffolding state.
	// Note: If 'git add' found nothing (e.g. empty dir), this might fail with "nothing to commit".
	// However, scbake always creates scbake.toml at minimum, so this should usually succeed.
	prio, err = seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "git",
		Args:        []string{"commit", "-m", "scbake: Apply templates"},
		Desc:        "Create initial commit",
		TaskPrio:    int(prio),
		RunInTarget: true,
	})

	return plan, nil
}
