package cmd

import (
	"context"
	"fmt"
	"os"
	"scbake/internal/core"
	"scbake/internal/git"
	"scbake/internal/preflight"
	"scbake/internal/types"
	"scbake/pkg/lang"
	"scbake/pkg/tasks" // We still need this for the test plan

	"github.com/spf13/cobra"
)

// Define flags
var langFlag string

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
		if err := runApply(cmd, args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Success! 'apply' command finished.")
	},
}

// buildPlan constructs the list of tasks based on CLI flags.
func buildPlan() (*types.Plan, string, error) {
	plan := &types.Plan{Tasks: []types.Task{}}
	commitMessage := "scbake: Apply templates"

	// --- Language Handler ---
	if langFlag != "" {
		// Run pre-flight check for the language
		if langFlag == "go" {
			if err := preflight.CheckBinaries("go"); err != nil {
				return nil, "", err
			}
		}

		// Get the handler
		handler, err := lang.GetHandler(langFlag)
		if err != nil {
			return nil, "", err
		}

		// Get tasks from the handler
		langTasks, err := handler.GetTasks()
		if err != nil {
			return nil, "", fmt.Errorf("failed to get tasks for lang '%s': %w", langFlag, err)
		}
		plan.Tasks = append(plan.Tasks, langTasks...)
		commitMessage = fmt.Sprintf("scbake: Apply '%s' language pack", langFlag)

	} else {
		// This is the default test plan if no flags are given
		// We'll remove this once the --with flag is implemented
		plan.Tasks = append(plan.Tasks, &tasks.ExecCommandTask{
			Cmd:      "echo",
			Args:     []string{"'Hello from scbake test plan'"},
			Desc:     "Run test echo command",
			TaskPrio: 100,
		})
		commitMessage = "scbake: Apply test plan"
	}

	// --- Tooling Templates (e.g., --with) ---
	// We will add this logic in a future commit.

	if len(plan.Tasks) == 0 {
		return nil, "", fmt.Errorf("no language or templates specified. Use --lang or --with")
	}

	return plan, commitMessage, nil
}

func runApply(cmd *cobra.Command, args []string) error {
	// 1. =========== PRE-FLIGHT & SAFETY CHECKS ===========
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
	fmt.Println("[2/5] 📝 Building execution plan...")
	plan, commitMessage, err := buildPlan()
	if err != nil {
		return err
	}

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
		return core.Execute(plan, tc)
	}

	// 3. =========== CREATE SAVEPOINT ===========
	fmt.Println("[3/5] 🛡️  Creating Git savepoint...")
	savepointTag, err := git.CreateSavepoint()
	if err != nil {
		return fmt.Errorf("failed to create savepoint: %w", err)
	}

	// 4. =========== EXECUTE THE PLAN ===========
	fmt.Println("[4/5] 🚀 Executing plan...")
	if err := core.Execute(plan, tc); err != nil {
		// 4a. ROLLBACK ON FAILURE
		fmt.Fprintf(os.Stderr, "⚠️ Task execution failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Task failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("operation rolled back")
	}

	// 5. =========== COMMIT ON SUCCESS ===========
	fmt.Println("[5/5] 💾 Committing changes...")
	if err := git.CommitChanges(commitMessage); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Commit failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Commit failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("commit failed, operation rolled back")
	}

	// 6. =========== CLEANUP ===========
	// We now have 6 steps. Let's fix logging in a future UI commit.
	fmt.Println("[6/6] 🧹 Cleaning up savepoint...")
	if err := git.DeleteSavepoint(savepointTag); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to delete savepoint tag '%s'. You may want to remove it manually.\n", savepointTag)
	}

	return nil
}

func init() {
	// This line was already present in the old `init()`
	rootCmd.AddCommand(applyCmd)

	// Add our new persistent flags
	applyCmd.PersistentFlags().StringVar(&langFlag, "lang", "", "Language project pack to apply (e.g., 'go')")
	// We'll add --with here later
}
