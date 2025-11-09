// Package tasks defines the executable units of work used in a scaffolding plan.
package tasks

import (
	"bytes"
	"fmt"
	"os/exec"
	"scbake/internal/types"
)

// ExecCommandTask runs an external shell command.
type ExecCommandTask struct {
	// The command to run (e.g., "go")
	Cmd string

	// The arguments (e.g., "mod", "tidy")
	Args []string

	// The human-readable description
	Desc string

	// The execution priority
	TaskPrio int

	// If true, run in TaskContext.TargetPath, else run in "."
	RunInTarget bool
}

// Description returns a human-readable summary of the task.
func (t *ExecCommandTask) Description() string {
	return t.Desc
}

// Priority returns the execution priority level.
func (t *ExecCommandTask) Priority() int {
	return t.TaskPrio
}

// Execute performs the command execution task.
func (t *ExecCommandTask) Execute(tc types.TaskContext) error {
	if tc.DryRun {
		// In dry-run, we just log what we *would* have done.
		return nil
	}

	// The command and arguments must be carefully controlled via the manifest to prevent injection.
	cmd := exec.CommandContext(tc.Ctx, t.Cmd, t.Args...)

	// Set the working directory for the command
	if t.RunInTarget {
		cmd.Dir = tc.TargetPath
	} else {
		cmd.Dir = "."
	}

	// We capture stderr to provide better error messages
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to run command '%s %v': %v\nstderr: %s", t.Cmd, t.Args, err, stderr.String())
	}

	return nil
}
