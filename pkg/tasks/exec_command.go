// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"scbake/internal/templateregistry"
	"scbake/internal/types"
	"slices"
	"strings"
	"time"
)

const (
	registryTokenEnvPrefix = templateregistry.RegistryTokenEnvPrefix
	maxStderrBytes         = 4 << 10

	// commandWaitDelay bounds how long Wait may block on inherited I/O pipes
	// after the child is killed. Orphaned descendants can otherwise keep the
	// stderr pipe open indefinitely (os/exec Cmd.WaitDelay).
	commandWaitDelay = 2 * time.Second
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

	// PredictedCreated lists files or directories this command may create or
	// modify. Despite the historical name, it is not limited to missing paths:
	// when a transaction is active, every listed path is tracked before the
	// command runs. Tracking records a missing path for deletion, backs up an
	// existing regular file (content and mode), and leaves existing directories
	// unchanged. Relative paths are resolved against the command's working
	// directory. This lets rollback undo opaque command side effects such as
	// `go mod tidy` rewriting go.mod or `npm install` changing package.json.
	PredictedCreated []string

	// ArchivePath is an optional zip archive whose entries are tracked before
	// execution. It is intended for extraction commands such as `unzip`: each
	// safe entry destination and its missing ancestors are registered with the
	// transaction so nested files overwritten or created by the command can be
	// restored or removed after a later failure.
	ArchivePath string
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

	snapshotPaths, err := t.trackSideEffects(tc)
	if err != nil {
		return err
	}

	// The command and arguments must be carefully controlled via the manifest to prevent injection.
	// G204: Command arguments are defined in the task manifest, which is the intended behavior
	//nolint:gosec
	cmd := exec.CommandContext(tc.Ctx, t.Cmd, t.Args...)
	cmd.WaitDelay = commandWaitDelay

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

	runErr := cmd.Run()

	// Snapshot declared/archive destinations after the command, whether it
	// succeeded or failed, so rollback can verify the bytes it created before
	// removing them. Snapshot errors only warn: a successful command must not
	// fail because cleanup lost a hash, and the safe outcome is to keep it.
	snapshotCreatedContent(tc, snapshotPaths)

	if runErr != nil {
		return commandExecutionError(tc.Ctx, t.Cmd, t.Args, runErr, &stderr)
	}

	return nil
}

// snapshotCreatedContent records post-write hashes for paths the transaction
// created. It is best-effort: failures warn and leave the path unhashed, which
// makes rollback keep it.
func snapshotCreatedContent(tc types.TaskContext, paths []string) {
	if tc.Tx == nil {
		return
	}
	for _, path := range paths {
		if err := tc.Tx.SnapshotCreatedContent(path); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Could not snapshot created path %s: %v\n", path, err)
		}
	}
}

// trackSideEffects registers paths the command may create or modify before it
// runs. Tracking every prediction unconditionally means Track backs up an
// existing file, records a missing path for deletion, or no-ops an existing
// directory. Archive entries are expanded separately so nested writes are also
// covered. It returns the declared and archive destination paths so their
// created content can be snapshotted after the command runs.
func (t *ExecCommandTask) trackSideEffects(tc types.TaskContext) ([]string, error) {
	if tc.Tx == nil {
		return nil, nil
	}

	cmdDir, err := commandWorkingDir(tc, t.RunInTarget)
	if err != nil {
		return nil, err
	}
	predicted, err := trackPredictedPaths(tc, cmdDir, t.PredictedCreated)
	if err != nil {
		return nil, err
	}
	if t.ArchivePath == "" {
		return predicted, nil
	}
	archivePaths, err := trackArchiveEntriesWithDestinations(tc, cmdDir, t.ArchivePath)
	if err != nil {
		return nil, err
	}
	return append(predicted, archivePaths...), nil
}

// trackPredictedPaths resolves each prediction against the command's working
// directory and registers it with the transaction. Absolute predictions are
// validated by Track against the transaction root rather than joined again. It
// returns the resolved paths for post-command snapshots.
func trackPredictedPaths(tc types.TaskContext, cmdDir string, paths []string) ([]string, error) {
	resolved := make([]string, 0, len(paths))
	for _, pred := range paths {
		fullPath := pred
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(cmdDir, pred)
		}
		if err := tc.Tx.Track(fullPath); err != nil {
			return nil, fmt.Errorf("failed to track predicted path %s: %w", fullPath, err)
		}
		resolved = append(resolved, fullPath)
	}
	return resolved, nil
}

// commandWorkingDir returns the absolute directory the command will run in, so
// relative predictions and archive paths are interpreted the same way the
// child process interprets them.
func commandWorkingDir(tc types.TaskContext, runInTarget bool) (string, error) {
	if runInTarget {
		abs, err := filepath.Abs(tc.TargetPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve target directory %s: %w", tc.TargetPath, err)
		}
		return abs, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to resolve process working directory: %w", err)
	}
	return cwd, nil
}

// commandExecutionError turns a child failure into a user-facing error. It also
// treats ErrWaitDelay as success: WaitDelay only closes inherited pipes after
// the child has already exited successfully.
func commandExecutionError(ctx context.Context, name string, args []string, err error, stderr *limitedBuffer) error {
	if ctxErr := canceledCommandError(ctx); ctxErr != nil {
		return fmt.Errorf("command '%s %v' interrupted: %w", name, args, ctxErr)
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}

	msg := stderr.buf.String()
	if stderr.truncated {
		msg += "\n[stderr truncated]"
	}
	return fmt.Errorf("failed to run command '%s %v': %v\nstderr: %s", name, args, err, msg)
}

// canceledCommandError reports a canceled task context, tolerating the zero
// TaskContext used by tests and library callers that do not configure one.
func canceledCommandError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
