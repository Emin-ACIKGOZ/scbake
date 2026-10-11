// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
)

// Handler implements the templates.Handler interface for Git.
type Handler struct{}

// GetTasks returns the plan to initialize a Git repository.
// It performs `git init` (only when the target is not already a repository),
// `git add .`, and `git commit`.
//
// Security (SCB-037): if the target already contains a `.git` directory, that
// repository is untrusted input. Running `git commit` inside it would execute
// any planted `.git/hooks/pre-commit` (or `core.hooksPath`) as the invoking
// user. To stay fail-closed we never re-run `git init` on an existing repo and
// we always point `core.hooksPath` at a path that cannot contain hooks before
// invoking `add`/`commit`.
func (h *Handler) GetTasks(targetPath string, _ string, _ string) ([]types.Task, error) {
	var plan []types.Task

	// Use PrioritySequence to avoid magic numbers and ensure strictly ordered execution
	// within the VersionControl band.
	seq, err := types.NewPrioritySequence(types.PrioVersionControl, types.MaxVersionControl)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	// Determine whether the target is already a Git repository. If so, we must
	// not re-initialize it (which could clobber an untrusted config) and the
	// repository's hooks must be treated as hostile.
	repoExists := false
	if targetPath != "" {
		if _, statErr := os.Stat(filepath.Join(targetPath, fileutil.GitDir)); statErr == nil {
			repoExists = true
		}
	}

	// core.hooksPath points at a path that cannot contain hooks: os.DevNull is
	// "/dev/null" on Unix and "NUL" on Windows. Git then finds no hook to run,
	// so a planted pre-commit hook is ignored on every supported platform.
	noHooks := "core.hooksPath=" + os.DevNull

	// Task 1: Initialize Git repository (only for a fresh target).
	// We predict creation of GitDir so the transaction system can rollback the entire repo creation on failure.
	if !repoExists {
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
			PredictedCreated: []string{fileutil.GitDir},
		})
	}

	// Task 2: Stage all files with hooks disabled.
	prio, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "git",
		Args:        []string{"-c", noHooks, "add", "."},
		Desc:        "Stage files",
		TaskPrio:    int(prio),
		RunInTarget: true,
	})

	// Task 3: Create initial commit with hooks disabled.
	// We use "commit -m ..." to snapshot the scaffolding state.
	// Note: If 'git add' found nothing (e.g. empty dir), this might fail with "nothing to commit".
	// However, scbake always creates the manifest at minimum, so this should usually succeed.
	prio, err = seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "git",
		Args:        []string{"-c", noHooks, "commit", "--allow-empty", "-m", "scbake: Apply templates"},
		Desc:        "Create initial commit",
		TaskPrio:    int(prio),
		RunInTarget: true,
		// Explicitly track internal mutations if they don't already exist.
		PredictedCreated: []string{fileutil.GitDir},
	})

	return plan, nil
}
