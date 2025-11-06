package cmd

import (
	"context"
	"fmt"
	"os"
	"scbake/internal/core"
	"scbake/internal/git"
	"scbake/internal/manifest"
	"scbake/internal/preflight"
	"scbake/internal/types"
	"scbake/pkg/lang"
	"scbake/pkg/tasks"
	"scbake/pkg/templates" // Import the new template registry

	"github.com/spf13/cobra"
)

// Define flags
var (
	langFlag string
	withFlag []string // Slice to accept multiple --with flags
)

// applyCmd represents the apply command
var applyCmd = &cobra.Command{
	Use:   "apply [--lang <lang>] [--with <template...>] [<path>]",
	Short: "Apply a language pack or tooling template to a project",
	Long:  `...`, // (description unchanged)
	Run: func(cmd *cobra.Command, args []string) {
		if err := runApply(cmd, args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Success! 'apply' command finished.")
	},
}

// buildPlan constructs the list of tasks based on CLI flags.
func buildPlan(m *types.Manifest) (*types.Plan, string, error) {
	plan := &types.Plan{Tasks: []types.Task{}}
	commitMessage := "scbake: Apply templates"
	didSomething := false

	// --- Language Handler ---
	if langFlag != "" {
		didSomething = true
		if langFlag == "go" {
			if err := preflight.CheckBinaries("go"); err != nil {
				return nil, "", err
			}
		}

		handler, err := lang.GetHandler(langFlag)
		if err != nil {
			return nil, "", err
		}

		langTasks, err := handler.GetTasks()
		if err != nil {
			return nil, "", fmt.Errorf("failed to get tasks for lang '%s': %w", langFlag, err)
		}
		plan.Tasks = append(plan.Tasks, langTasks...)

		newProject := &types.Project{
			Name:     langFlag,
			Path:     ".", // We'll fix this in the next commit
			Language: langFlag,
		}
		plan.Tasks = append(plan.Tasks, &tasks.UpdateManifestTask{
			NewProject: newProject,
			Desc:       "Update scbake.toml with new project",
			TaskPrio:   998,
		})
		commitMessage = fmt.Sprintf("scbake: Apply '%s' language pack", langFlag)
	}

	// --- Tooling Templates (e.g., --with) ---
	if len(withFlag) > 0 {
		didSomething = true
		var appliedTemplates []string

		for _, tmplName := range withFlag {
			handler, err := templates.GetHandler(tmplName)
			if err != nil {
				return nil, "", err
			}
			tmplTasks, err := handler.GetTasks()
			if err != nil {
				return nil, "", fmt.Errorf("failed to get tasks for template '%s': %w", tmplName, err)
			}
			plan.Tasks = append(plan.Tasks, tmplTasks...)
			appliedTemplates = append(appliedTemplates, tmplName)
		}

		// Add a task to update the manifest with the applied templates
		// This assumes we are applying to the root.
		plan.Tasks = append(plan.Tasks, &tasks.UpdateManifestTask{
			NewTemplate: &types.Template{Name: "root-templates", Path: "."}, // This is a bit of a hack for now
			Desc:        "Update scbake.toml with new templates",
			TaskPrio:    998,
		})

		// We'll improve this message logic later
		commitMessage = fmt.Sprintf("scbake: Apply templates (%v)", withFlag)
	}

	if !didSomething {
		return nil, "", fmt.Errorf("no language or templates specified. Use --lang or --with")
	}

	return plan, commitMessage, nil
}

func runApply(cmd *cobra.Command, args []string) error {
	// (This function's content is identical to Commit 15)
	// 1. =========== PRE-FLIGHT & SAFETY CHECKS ===========
	if !dryRun {
		fmt.Println("[1/6] 🔎 Running Git pre-flight checks...")
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

	// 2. =========== LOAD MANIFEST ===========
	fmt.Println("[2/6] 📖 Loading manifest (scbake.toml)...")
	m, err := manifest.Load()
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", manifest.ManifestFileName, err)
	}

	// 3. =========== BUILD THE PLAN ===========
	fmt.Println("[3/6] 📝 Building execution plan...")
	plan, commitMessage, err := buildPlan(m)
	if err != nil {
		return err
	}

	// Build the Task Context, NOW WITH THE MANIFEST
	tc := types.TaskContext{
		Ctx:      context.Background(),
		DryRun:   dryRun,
		Manifest: m, // Pass the loaded manifest
		// TargetPath logic will be added in Commit 19
	}

	// If dry-run, just execute the plan (which will just print) and exit
	if dryRun {
		fmt.Println("DRY RUN: No changes will be made.")
		fmt.Println("Plan contains the following tasks:")
		return core.Execute(plan, tc)
	}

	// 4. =========== CREATE SAVEPOINT ===========
	fmt.Println("[4/6] 🛡️  Creating Git savepoint...")
	savepointTag, err := git.CreateSavepoint()
	if err != nil {
		return fmt.Errorf("failed to create savepoint: %w", err)
	}

	// 5. =========== EXECUTE THE PLAN ===========
	fmt.Println("[5/6] 🚀 Executing plan...")
	if err := core.Execute(plan, tc); err != nil {
		// 5a. ROLLBACK ON FAILURE
		fmt.Fprintf(os.Stderr, "⚠️ Task execution failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Task failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("operation rolled back")
	}

	// 6. =========== COMMIT ON SUCCESS ===========
	fmt.Println("[6/6] 💾 Committing changes...")
	if err := git.CommitChanges(commitMessage); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Commit failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Commit failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("commit failed, operation rolled back")
	}

	// 7. =========== CLEANUP ===========
	fmt.Println("[7/7] 🧹 Cleaning up savepoint...")
	if err := git.DeleteSavepoint(savepointTag); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to delete savepoint tag '%s'. You may want to remove it manually.\n", savepointTag)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(applyCmd)

	applyCmd.PersistentFlags().StringVar(&langFlag, "lang", "", "Language project pack to apply (e.g., 'go')")
	// Add the --with flag, allowing it to be used multiple times
	applyCmd.PersistentFlags().StringSliceVar(&withFlag, "with", []string{}, "Tooling template to apply (e.g., 'makefile')")
}
