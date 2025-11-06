package core

import (
	"context"
	"fmt"
	"os"
	"scbake/internal/git"
	"scbake/internal/manifest"
	"scbake/internal/preflight"
	"scbake/internal/types"
	"scbake/internal/util" // Import new package
	"scbake/pkg/lang"
	"scbake/pkg/templates"
)

// StepLogger (unchanged)
type StepLogger struct {
	currentStep int
	totalSteps  int // Keep unexported
	DryRun      bool
}

func NewStepLogger(totalSteps int, dryRun bool) *StepLogger {
	return &StepLogger{totalSteps: totalSteps, DryRun: dryRun}
}
func (l *StepLogger) Log(emoji, message string) {
	l.currentStep++
	if l.DryRun && l.currentStep > 2 {
		return
	}
	fmt.Printf("[%d/%d] %s %s\n", l.currentStep, l.totalSteps, emoji, message)
}
func (l *StepLogger) SetTotalSteps(newTotal int) {
	l.totalSteps = newTotal
}

// RunContext (unchanged)
type RunContext struct {
	LangFlag   string
	WithFlag   []string
	TargetPath string
	DryRun     bool
	Force      bool
}

// manifestChanges (unchanged)
type manifestChanges struct {
	Projects  []types.Project
	Templates []types.Template
}

// RunApply (unchanged)
func RunApply(rc RunContext) error {
	logger := NewStepLogger(9, rc.DryRun)

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

	logger.Log("📖", "Loading manifest (scbake.toml)...")
	m, err := manifest.Load()
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", manifest.ManifestFileName, err)
	}

	logger.Log("📝", "Building execution plan...")
	plan, commitMessage, changes, err := buildPlan(rc)
	if err != nil {
		return err
	}

	futureManifest := *m
	futureManifest.Projects = append(futureManifest.Projects, changes.Projects...)
	futureManifest.Templates = append(futureManifest.Templates, changes.Templates...)

	tc := types.TaskContext{
		Ctx:        context.Background(),
		DryRun:     rc.DryRun,
		Manifest:   &futureManifest,
		TargetPath: rc.TargetPath,
		Force:      rc.Force,
	}

	if rc.DryRun {
		fmt.Println("DRY RUN: No changes will be made.")
		fmt.Println("Plan contains the following tasks:")
		return Execute(plan, tc)
	}

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

	logger.Log("🛡️", "Creating Git savepoint...")
	savepointTag, err := git.CreateSavepoint()
	if err != nil {
		return fmt.Errorf("failed to create savepoint: %w", err)
	}

	logger.Log("🚀", "Executing plan...")
	if err := Execute(plan, tc); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Task execution failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Task failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("operation rolled back")
	}

	logger.Log("✍️", "Updating manifest...")
	existingProjects := make(map[string]bool)
	for _, proj := range m.Projects {
		existingProjects[proj.Path] = true
	}
	for _, newProj := range changes.Projects {
		if !existingProjects[newProj.Path] {
			m.Projects = append(m.Projects, newProj)
		}
	}
	existingTemplates := make(map[string]bool)
	for _, tmpl := range m.Templates {
		key := tmpl.Name + ":" + tmpl.Path
		existingTemplates[key] = true
	}
	for _, newTmpl := range changes.Templates {
		key := newTmpl.Name + ":" + newTmpl.Path
		if !existingTemplates[key] {
			m.Templates = append(m.Templates, newTmpl)
		}
	}

	if err := manifest.Save(m); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Manifest save failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Manifest save failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("manifest save failed, operation rolled back")
	}

	logger.Log("💾", "Committing changes...")
	if err := git.CommitChanges(commitMessage); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Commit failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
			return fmt.Errorf("CRITICAL: Commit failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
		}
		return fmt.Errorf("commit failed, operation rolled back")
	}

	logger.SetTotalSteps(10)
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

		// --- THIS IS THE FIX ---
		// Sanitize the project name for the manifest
		projectName, err := util.SanitizeModuleName(rc.TargetPath)
		if err != nil {
			return nil, "", nil, fmt.Errorf("could not determine project name: %w", err)
		}
		// --- END FIX ---

		changes.Projects = append(changes.Projects, types.Project{
			Name:     projectName, // Use sanitized name
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

		changes.Templates = append(changes.Templates, types.Template{
			Name: "root-templates",
			Path: rc.TargetPath,
		})
		commitMessage = fmt.Sprintf("scbake: Apply templates (%v) to %s", rc.WithFlag, rc.TargetPath)
	}

	if !didSomething {
		return nil, "", nil, fmt.Errorf("no language or templates specified. Use --lang or --with")
	}

	return plan, commitMessage, changes, nil
}
