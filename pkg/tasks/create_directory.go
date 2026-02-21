// Package tasks defines the executable units of work used in a scaffolding plan.
package tasks

import (
	"fmt"
	"os"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// CreateDirTask ensures a directory exists.
type CreateDirTask struct {
	Path     string
	Desc     string
	TaskPrio int
}

// Execute performs the task of creating the directory.
func (t *CreateDirTask) Execute(_ types.TaskContext) error {
	// Use the constant from the centralized location
	if err := os.MkdirAll(t.Path, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", t.Path, err)
	}
	return nil
}

// Description returns a human-readable summary of the task.
func (t *CreateDirTask) Description() string { return t.Desc }

// Priority returns the execution priority level.
func (t *CreateDirTask) Priority() int { return t.TaskPrio }
