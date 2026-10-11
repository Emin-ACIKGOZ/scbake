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

// gitCmd is the Git executable invoked for repository setup tasks.
const gitCmd = "git"

// GetTasks returns the plan to initialize a Git repository.
// It performs `git init`, `git add .`, and `git commit` for a fresh target.
//
// Security (SCB-037): a pre-existing `.git` entry is untrusted input. Even
// with hooks disabled via `-c core.hooksPath=<devnull>`, invoking Git inside an
// attacker-controlled repository still executes attacker code through
// `filter.*.clean` (run by `git add`) and `core.fsmonitor` (run by `git add`
// and `git status`), both read from the untrusted `.git/config`. Disabling hooks
// is therefore only a partial mitigation.
//
// To stay fail-closed we schedule NO Git tasks at all when the target already
// contains a `.git` entry: we never run `git init`, `git add`, or
// `git commit` inside a repository we did not create. A fresh target (no `.git`)
// is initialized and committed with hooks disabled, and we keep the transaction
// tracking that predicts creation of `.git`.
func (h *Handler) GetTasks(targetPath string, _ string, _ string) ([]types.Task, error) {
	// Determine whether the target is already a Git repository. If so, treat the
	// repository as hostile and refuse to run any Git command inside it.
	//
	// We use os.Lstat, not os.Stat: Stat follows symlinks, so a `.git` that is a
	// dangling symlink (or a symlink whose chain dangles) would look absent and
	// let us schedule `git init`. Git would then follow that symlink and create
	// the repository outside the target, or a swapped symlink could point at an
	// attacker-controlled repo whose `filter.*.clean`/`core.fsmonitor` execute.
	// Lstat observes the link itself, so any `.git` entry (dangling symlink,
	// symlink to anywhere, regular file, or directory) is treated as a
	// pre-existing repository and no Git tasks are scheduled (fail closed).
	if targetPath != "" {
		if _, statErr := os.Lstat(filepath.Join(targetPath, fileutil.GitDir)); statErr == nil {
			return nil, nil
		}
	}

	var plan []types.Task

	// Use PrioritySequence to avoid magic numbers and ensure strictly ordered execution
	// within the VersionControl band.
	seq, err := types.NewPrioritySequence(types.PrioVersionControl, types.MaxVersionControl)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	// core.hooksPath points at a path that cannot contain hooks: os.DevNull is
	// "/dev/null" on Unix and "NUL" on Windows. Git then finds no hook to run,
	// so a planted pre-commit hook is ignored on every supported platform.
	noHooks := "core.hooksPath=" + os.DevNull

	// Task 1: Initialize Git repository (only for a fresh target).
	// We predict creation of GitDir so the transaction system can rollback the entire repo creation on failure.
	prio, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:              gitCmd,
		Args:             []string{"init"},
		Desc:             "Initialize Git repository",
		TaskPrio:         int(prio),
		RunInTarget:      true,
		PredictedCreated: []string{fileutil.GitDir},
	})

	// Task 2: Stage all files with hooks disabled.
	prio, err = seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         gitCmd,
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
		Cmd:         gitCmd,
		Args:        []string{"-c", noHooks, "commit", "--allow-empty", "-m", "scbake: Apply templates"},
		Desc:        "Create initial commit",
		TaskPrio:    int(prio),
		RunInTarget: true,
		// Explicitly track internal mutations if they don't already exist.
		PredictedCreated: []string{fileutil.GitDir},
	})

	return plan, nil
}
