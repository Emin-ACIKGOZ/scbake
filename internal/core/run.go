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
	"scbake/pkg/templates"
)

// StepLogger helps print consistent step messages
type StepLogger struct {
	currentStep int
	totalSteps  int // Keep unexported
	DryRun      bool
}

func NewStepLogger(totalSteps int, dryRun bool) *StepLogger {
	return &StepLogger{totalSteps: totalSteps, DryRun: dryRun} // Use unexported field
}

func (l *StepLogger) Log(emoji, message string) {
	l.currentStep++
	if l.DryRun && l.currentStep > 2 { // Only log first few steps in dry run
		return
	}
	fmt.Printf("[%d/%d] %s %s\n", l.currentStep, l.totalSteps, emoji, message) // Use unexported field
}

// SetTotalSteps allows external callers to update the step count.
func (l *StepLogger) SetTotalSteps(newTotal int) {
	l.totalSteps = newTotal
}

// RunContext holds all the flags and args for a run.
type RunContext struct {
	LangFlag   string
	WithFlag   []string
	TargetPath string
	DryRun     bool
	Force      bool
}

// A struct to hold all proposed manifest changes
type manifestChanges struct {
	Projects  []types.Project
	Templates []types.Template
}

// RunApply is the main logic for the 'apply' command, extracted.
func RunApply(rc RunContext) error {
	// We have 9 steps in the apply logic
	logger := NewStepLogger(9, rc.DryRun)

	// 1. =========== PRE-FLIGHT & SAFETY CHECKS ===========
	if !rc.DryRun {
		logger.Log("🔎", "Running Git pre-flight checks...")
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
	logger.Log("📖", "Loading manifest (scbake.toml)...")
	m, err := manifest.Load()
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", manifest.ManifestFileName, err)
	}

	// 3. =========== BUILD THE PLAN ===========
	logger.Log("📝", "Building execution plan...")
	plan, commitMessage, changes, err := buildPlan(rc)
	if err != nil {
		return err
	}

	// Build the Task Context
	tc := types.TaskContext{
		Ctx:        context.Background(),
		DryRun:     rc.DryRun,
		Manifest:   m, // This is still the *original* manifest
		TargetPath: rc.TargetPath,
		Force:      rc.Force,
	}

	// If dry-run, just execute the plan and exit
	if rc.DryRun {
		fmt.Println("DRY RUN: No changes will be made.")
		fmt.Println("Plan contains the following tasks:")
		return Execute(plan, tc)
	}

	// 4. =========== FIX "HEAD REF" BUG ===========
	// Check if HEAD is valid. If not, create an initial commit.
	hasHEAD, err := git.CheckHasHEAD()
	if err != nil {
		return fmt.Errorf("failed to check for HEAD: %w", err)
	}
	if !hasHEAD {
		logger.Log("GIT", "Creating initial commit...")
		if err := git.InitialCommit("scbake: Initial commit"); err != nil {
			return err
		}
	}

	// 5. =========== CREATE SAVEPOINT ===========
	logger.Log("🛡️", "Creating Git savepoint...")
	savepointTag, err := git.CreateSavepoint()
	if err != nil {
		return fmt.Errorf("failed to create savepoint: %w", err)
	}

	// 6. =========== EXECUTE THE PLAN ===========
	logger.Log("🚀", "Executing plan...")
	if err := Execute(plan, tc); err != nil {
		// 6a. ROLLBACK ON FAILURE
		fmt.Fprintf(os.Stderr, "⚠️ Task execution failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Task failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("operation rolled back")
	}

	// 7. =========== UPDATE & SAVE MANIFEST ===========
	logger.Log("✍️", "Updating manifest...")
	// Apply changes *after* tasks succeed
	m.Projects = append(m.Projects, changes.Projects...)
	m.Templates = append(m.Templates, changes.Templates...)

	if err := manifest.Save(m); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Manifest save failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Manifest save failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("manifest save failed, operation rolled back")
	}

	// 8. =========== COMMIT ON SUCCESS ===========
	logger.Log("💾", "Committing changes...")
	if err := git.CommitChanges(commitMessage); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Commit failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Commit failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("commit failed, operation rolled back")
	}

	// 9. =========== CLEANUP ===========
	logger.totalSteps = 9 // This is still unexported, so we're just updating it here
	logger.Log("🧹", "Cleaning up savepoint...")
	if err := git.DeleteSavepoint(savepointTag); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to delete savepoint tag '%s'. You may want to remove it manually.\n", savepointTag)
	}

	return nil
}

// buildPlan constructs the list of tasks based on CLI flags.
func buildPlan(rc RunContext) (*types.Plan, string, *manifestChanges, error) {
	plan := &types.Plan{Tasks: []types.Task{}}
	changes := &manifestChanges{}
	commitMessage := "scbake: Apply templates"
	didSomething := false

	if rc.LangFlag != "" {
		didSomething = true
		if rc.LangFlag == "go" {
			if err := preflight.CheckBinaries("go"); err != nil {
				return nil, "", nil, err
			}
		}
		handler, err := lang.GetHandler(rc.LangFlag)
		if err != nil {
			return nil, "", nil, err
		}
		langTasks, err := handler.GetTasks(rc.TargetPath)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to get tasks for lang '%s': %w", rc.LangFlag, err)
		}
		plan.Tasks = append(plan.Tasks, langTasks...)

		// Add to the list of changes, don't add a task
		changes.Projects = append(changes.Projects, types.Project{
			Name:     filepath.Base(rc.TargetPath),
			Path:     rc.TargetPath,
			Language: rc.LangFlag,
		})
		commitMessage = fmt.Sprintf("scbake: Apply '%s' to %s", rc.LangFlag, rc.TargetPath)
	}

	if len(rc.WithFlag) > 0 {
		didSomething = true
		for _, tmplName := range rc.WithFlag {
			handler, err := templates.GetHandler(tmplName)
			if err != nil {
				return nil, "", nil, err
			}
			tmplTasks, err := handler.GetTasks(rc.TargetPath)
			if err != nil {
				return nil, "", nil, fmt.Errorf("failed to get tasks for template '%s': %w", tmplName, err)
			}
			plan.Tasks = append(plan.Tasks, tmplTasks...)
		}

		// Add to the list of changes, don't add a task
		changes.Templates = append(changes.Templates, types.Template{
			Name: "root-templates", // This logic can be improved later
			Path: rc.TargetPath,
		})
		commitMessage = fmt.Sprintf("scbake: Apply templates (%v) to %s", rc.WithFlag, rc.TargetPath)
	}

	if !didSomething {
		return nil, "", nil, fmt.Errorf("no language or templates specified. Use --lang or --with")
	}

	return plan, commitMessage, changes, nil
}
