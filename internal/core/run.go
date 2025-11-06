package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/git"
	"scbake/internal/manifest"
	"scbake/internal/preflight"
	"scbake/internal/types"
	"scbake/pkg/lang"
	"scbake/pkg/tasks"
	"scbake/pkg/templates"
)

// RunContext holds all the flags and args for a run.
type RunContext struct {
	LangFlag   string
	WithFlag   []string
	TargetPath string
	DryRun     bool
}

// RunApply is the main logic for the 'apply' command, extracted.
func RunApply(rc RunContext) error {
	// 1. =========== PRE-FLIGHT & SAFETY CHECKS ===========
	if !rc.DryRun {
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
	plan, commitMessage, err := buildPlan(m, rc)
	if err != nil {
		return err
	}

	// Build the Task Context
	tc := types.TaskContext{
		Ctx:        context.Background(),
		DryRun:     rc.DryRun,
		Manifest:   m,
		TargetPath: rc.TargetPath,
	}

	// If dry-run, just execute the plan (which will just print) and exit
	if rc.DryRun {
		fmt.Println("DRY RUN: No changes will be made.")
		fmt.Println("Plan contains the following tasks:")
		return Execute(plan, tc)
	}

	// 4. =========== CREATE SAVEPOINT ===========
	fmt.Println("[4/6] 🛡️  Creating Git savepoint...")
	savepointTag, err := git.CreateSavepoint()
	if err != nil {
		return fmt.Errorf("failed to create savepoint: %w", err)
	}

	// 5. =========== EXECUTE THE PLAN ===========
	fmt.Println("[5/6] 🚀 Executing plan...")
	if err := Execute(plan, tc); err != nil {
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

// buildPlan constructs the list of tasks based on CLI flags.
func buildPlan(m *types.Manifest, rc RunContext) (*types.Plan, string, error) {
	plan := &types.Plan{Tasks: []types.Task{}}
	commitMessage := "scbake: Apply templates"
	didSomething := false

	// --- Language Handler ---
	if rc.LangFlag != "" {
		didSomething = true
		if rc.LangFlag == "go" {
			if err := preflight.CheckBinaries("go"); err != nil {
				return nil, "", err
			}
		}

		handler, err := lang.GetHandler(rc.LangFlag)
		if err != nil {
			return nil, "", err
		}

		langTasks, err := handler.GetTasks() // We'll refactor this next
		if err != nil {
			return nil, "", fmt.Errorf("failed to get tasks for lang '%s': %w", rc.LangFlag, err)
		}
		plan.Tasks = append(plan.Tasks, langTasks...)

		newProject := &types.Project{
			Name:     filepath.Base(rc.TargetPath),
			Path:     rc.TargetPath,
			Language: rc.LangFlag,
		}
		plan.Tasks = append(plan.Tasks, &tasks.UpdateManifestTask{
			NewProject: newProject,
			Desc:       fmt.Sprintf("Update scbake.toml with new project: %s", rc.TargetPath),
			TaskPrio:   998,
		})
		commitMessage = fmt.Sprintf("scbake: Apply '%s' to %s", rc.LangFlag, rc.TargetPath)
	}

	// --- Tooling Templates (e.g., --with) ---
	if len(rc.WithFlag) > 0 {
		didSomething = true
		for _, tmplName := range rc.WithFlag {
			handler, err := templates.GetHandler(tmplName)
			if err != nil {
				return nil, "", err
			}
			tmplTasks, err := handler.GetTasks() // We'll refactor this next
			if err != nil {
				return nil, "", fmt.Errorf("failed to get tasks for template '%s': %w", tmplName, err)
			}
			plan.Tasks = append(plan.Tasks, tmplTasks...)
		}

		plan.Tasks = append(plan.Tasks, &tasks.UpdateManifestTask{
			NewTemplate: &types.Template{Name: "root-templates", Path: rc.TargetPath},
			Desc:        "Update scbake.toml with new templates",
			TaskPrio:    998,
		})

		commitMessage = fmt.Sprintf("scbake: Apply templates (%v) to %s", rc.WithFlag, rc.TargetPath)
	}

	if !didSomething {
		return nil, "", fmt.Errorf("no language or templates specified. Use --lang or --with")
	}

	return plan, commitMessage, nil
}
