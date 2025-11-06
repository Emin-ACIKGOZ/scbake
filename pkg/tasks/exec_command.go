package tasks

import (
	"bytes"
	"fmt"
	"os/exec"
	"scbake/internal/types"
)

// ExecCommandTask runs an external shell command.
type ExecCommandTask struct {
	Cmd         string   // The command to run (e.g., "go")
	Args        []string // The arguments (e.g., "mod", "tidy")
	Desc        string   // The human-readable description
	TaskPrio    int      // The execution priority
	RunInTarget bool     // If true, run in TaskContext.TargetPath, else run in "."
}

func (t *ExecCommandTask) Description() string {
	return t.Desc
}

func (t *ExecCommandTask) Priority() int {
	return t.TaskPrio
}

func (t *ExecCommandTask) Execute(tc types.TaskContext) error {
	if tc.DryRun {
		// In dry-run, we just log what we *would* have done.
		return nil
	}

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
