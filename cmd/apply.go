package cmd

import (
	"context"
	"fmt"
	"os"
	"scbake/internal/core"
	"scbake/internal/git"
	"scbake/internal/types"
	"scbake/pkg/tasks"

	"github.com/spf13/cobra"
)

// applyCmd represents the apply command
var applyCmd = &cobra.Command{
	Use:   "apply [--lang <lang>] [--with <template...>] [<path>]",
	Short: "Apply a language pack or tooling template to a project",
	Long: `Applies language packs or tooling templates to a specified path.

This command is atomic:
1. It runs safety checks (clean Git tree).
2. Creates a Git savepoint.
3. Executes the plan.
4. Commits on success or rolls back on failure.`,
	Run: func(cmd *cobra.Command, args []string) {
		// This is the main application logic
		if err := runApply(cmd, args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Success! 'apply' command finished.")
	},
}

func runApply(cmd *cobra.Command, args []string) error {
	// 1. =========== PRE-FLIGHT & SAFETY CHECKS ===========
	// In dry-run, we skip safety checks to allow inspection
	if !dryRun {
		fmt.Println("[1/5] 🔎 Running Git pre-flight checks...")
		if err := git.CheckGitInstalled(); err != nil {
			return err
		}
		if err := git.CheckIsRepo(); err != nil {
			return err
		}
		if err := git.CheckIsClean(); err != nil {
			return err
		}
	}

	// 2. =========== BUILD THE PLAN ===========
	// This is a temporary test plan.
	// We'll replace this in future commits when we have handlers.
	testPlan := &types.Plan{
		Tasks: []types.Task{
			&tasks.ExecCommandTask{
				Cmd:      "echo",
				Args:     []string{"'Hello from scbake test plan'"},
				Desc:     "Run test echo command",
				TaskPrio: 100,
			},
		},
	}
	commitMessage := "scbake: Apply test plan"

	// Build the Task Context
	tc := types.TaskContext{
		Ctx:      context.Background(),
		DryRun:   dryRun,
		Manifest: &types.Manifest{}, // Empty manifest for now
		// TargetPath logic will be added in Commit 19
	}

	// If dry-run, just execute the plan (which will just print) and exit
	if dryRun {
		fmt.Println("DRY RUN: No changes will be made.")
		fmt.Println("Plan contains the following tasks:")
		return core.Execute(testPlan, tc)
	}

	// 3. =========== CREATE SAVEPOINT ===========
	fmt.Println("[2/5] 🛡️  Creating Git savepoint...")
	savepointTag, err := git.CreateSavepoint()
	if err != nil {
		return fmt.Errorf("failed to create savepoint: %w", err)
	}

	// 4. =========== EXECUTE THE PLAN ===========
	fmt.Println("[3/5] 🚀 Executing plan...")
	if err := core.Execute(testPlan, tc); err != nil {
		// 4a. ROLLBACK ON FAILURE
		fmt.Fprintf(os.Stderr, "⚠️ Task execution failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Task failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("operation rolled back")
	}

	// 5. =========== COMMIT ON SUCCESS ===========
	fmt.Println("[4/5] 💾 Committing changes...")
	if err := git.CommitChanges(commitMessage); err != nil {
		// This is a tricky state. The plan worked, but commit failed.
		// We should still roll back to maintain atomicity.
		fmt.Fprintf(os.Stderr, "⚠️ Commit failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Commit failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("commit failed, operation rolled back")
	}

	// 6. =========== CLEANUP ===========
	fmt.Println("[5/5] 🧹 Cleaning up savepoint...")
	if err := git.DeleteSavepoint(savepointTag); err != nil {
		// This is not a fatal error, just an annoyance.
		fmt.Fprintf(os.Stderr, "Warning: Failed to delete savepoint tag '%s'. You may want to remove it manually.\n", savepointTag)
	}

	return nil
}

func init() {
	// We'll add our flags like --lang and --with here later.
}
