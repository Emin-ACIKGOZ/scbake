// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"scbake/internal/templateregistry"
	"scbake/internal/types"
	"slices"
	"strings"
)

const (
	registryTokenEnvPrefix = templateregistry.RegistryTokenEnvPrefix
	maxStderrBytes         = 4 << 10
)

// scrubRegistryTokens removes registry credentials from a child environment.
// Names are compared case-insensitively for Windows.
func scrubRegistryTokens(env []string) []string {
	out := slices.DeleteFunc(env, func(kv string) bool {
		key, _, _ := strings.Cut(kv, "=")
		return len(key) >= len(registryTokenEnvPrefix) &&
			strings.EqualFold(key[:len(registryTokenEnvPrefix)], registryTokenEnvPrefix)
	})
	if out == nil {
		return []string{}
	}
	return out
}

// limitedBuffer caps captured stderr and records whether it was truncated.
type limitedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if remaining := maxStderrBytes - b.buf.Len(); remaining < len(p) {
		b.truncated = true
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

// ExecCommandTask runs an external shell command.
type ExecCommandTask struct {
	// The command to run (e.g., "go")
	Cmd string

	// The arguments (e.g., "mod", "tidy")
	Args []string

	Desc string

	TaskPrio int

	// If true, run in TaskContext.TargetPath, else run in "."
	RunInTarget bool

	// PredictedCreated lists files or directories this command is expected to create.
	// If a transaction is active, these paths are tracked *before* execution.
	// This allows the rollback system to clean up artifacts (like node_modules)
	// even if the command is opaque.
	PredictedCreated []string
}

// Description implements types.Task.
func (t *ExecCommandTask) Description() string {
	return t.Desc
}

// Priority implements types.Task.
func (t *ExecCommandTask) Priority() int {
	return t.TaskPrio
}

// Execute performs the command execution task.
func (t *ExecCommandTask) Execute(tc types.TaskContext) error {
	if tc.DryRun {
		// In dry-run, we just log what we *would* have done.
		return nil
	}

	// Safety Tracking for Predicted Outputs
	if tc.Tx != nil {
		for _, pred := range t.PredictedCreated {
			// Resolve predicted path relative to TargetPath
			// (Commands usually run relative to where they are invoked)
			fullPath := filepath.Join(tc.TargetPath, pred)

			// Only track if it does NOT exist.
			// We want to clean up new artifacts, but avoiding backing up massive existing folders (like node_modules).
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				if err := tc.Tx.Track(fullPath); err != nil {
					return fmt.Errorf("failed to track predicted output %s: %w", fullPath, err)
				}
			}
		}
	}

	// The command and arguments must be carefully controlled via the manifest to prevent injection.
	// G204: Command arguments are defined in the task manifest, which is the intended behavior
	//nolint:gosec
	cmd := exec.CommandContext(tc.Ctx, t.Cmd, t.Args...)

	// Set the working directory for the command
	if t.RunInTarget {
		cmd.Dir = tc.TargetPath
	} else {
		cmd.Dir = "."
	}

	// Children must not inherit registry credentials.
	cmd.Env = scrubRegistryTokens(cmd.Environ())

	// We capture stderr to provide better error messages
	var stderr limitedBuffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		msg := stderr.buf.String()
		if stderr.truncated {
			msg += "\n[stderr truncated]"
		}
		return fmt.Errorf("failed to run command '%s %v': %v\nstderr: %s", t.Cmd, t.Args, err, msg)
	}

	return nil
}
