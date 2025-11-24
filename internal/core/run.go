// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package core contains the central execution and planning logic.
package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"scbake/internal/git"
	"scbake/internal/manifest"
	"scbake/internal/preflight"
	"scbake/internal/types"
	"scbake/internal/util"
	"scbake/pkg/lang"
	"scbake/pkg/templates"
)

// Define constants for step logging and cyclomatic complexity reduction
const (
	runApplyTotalSteps  = 9
	langApplyTotalSteps = 10
)

// StepLogger helps print consistent step messages
type StepLogger struct {
	currentStep int
	totalSteps  int // Keep unexported
	DryRun      bool
}

// NewStepLogger creates a new StepLogger instance.
func NewStepLogger(totalSteps int, dryRun bool) *StepLogger {
	return &StepLogger{totalSteps: totalSteps, DryRun: dryRun}
}

// Log prints the current step message.
func (l *StepLogger) Log(emoji, message string) {
	l.currentStep++
	// Only print pre-flight checks in dry run (steps 1 and 2 of RunApply)
	if l.DryRun && l.currentStep > 2 {
		return
	}
	fmt.Printf("[%d/%d] %s %s\n", l.currentStep, l.totalSteps, emoji, message)
}

// SetTotalSteps updates the total number of steps for logging.
func (l *StepLogger) SetTotalSteps(newTotal int) {
	l.totalSteps = newTotal
}

// RunContext holds all the flags and args for a run.
type RunContext struct {
	LangFlag        string
	WithFlag        []string
	TargetPath      string // Used for execution (absolute path)
	ManifestPathArg string // Used for manifest/template rendering (relative path)
	DryRun          bool
	Force           bool
}

// A struct to hold all proposed manifest changes
type manifestChanges struct {
	Projects  []types.Project
	Templates []types.Template
}

// runGitPreflightChecks runs essential Git safety checks before modification.
func runGitPreflightChecks(logger *StepLogger) error {
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
	return nil
}

// RunApply is the main logic for the 'apply' command, extracted.
func RunApply(rc RunContext) error {
	logger := NewStepLogger(runApplyTotalSteps, rc.DryRun)

	if !rc.DryRun {
		if err := runGitPreflightChecks(logger); err != nil {
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

	// Prepare task context with proposed future manifest
	futureManifest := *m
	futureManifest.Projects = append(futureManifest.Projects, changes.Projects...)
	futureManifest.Templates = append(futureManifest.Templates, changes.Templates...)
	tc := types.TaskContext{
		Ctx:        context.Background(),
		DryRun:     rc.DryRun,
		Manifest:   &futureManifest,
		TargetPath: rc.TargetPath, // Use absolute path for execution
		Force:      rc.Force,
	}

	if rc.DryRun {
		fmt.Println("DRY RUN: No changes will be made.")
		fmt.Println("Plan contains the following tasks:")
		return Execute(plan, tc)
	}

	if err := ensureInitialCommit(logger); err != nil {
		return err
	}

	logger.Log("🛡️", "Creating Git savepoint...")
	savepointTag, err := git.CreateSavepoint()
	if err != nil {
		return fmt.Errorf("failed to create savepoint: %w", err)
	}

	if err := executeAndFinalize(logger, plan, tc, m, changes, savepointTag, commitMessage); err != nil { // Extracted core run logic
		return err
	}

	return nil
}

// ensureInitialCommit checks for HEAD and creates an initial commit if one is missing.
func ensureInitialCommit(logger *StepLogger) error {
	hasHEAD, err := git.CheckHasHEAD()
	if err != nil {
		return fmt.Errorf("failed to check for HEAD: %w", err)
	}
	if !hasHEAD {
		logger.Log("GIT", "Creating initial commit...")

		// Note: This relies on InitialCommit being called only once when starting from a fresh git init.
		if err := git.InitialCommit("scbake: Initial commit"); err != nil {
			return err
		}
	}
	return nil
}

// executeAndFinalize runs the plan, updates manifest, commits, and cleans up.
func executeAndFinalize(
	logger *StepLogger,
	plan *types.Plan,
	tc types.TaskContext,
	m *types.Manifest,
	changes *manifestChanges,
	savepointTag string,
	commitMessage string) error {
	logger.Log("🚀", "Executing plan...")
	if err := Execute(plan, tc); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Task execution failed: %v\n", err)
		return rollbackAndWrapError(savepointTag, errors.New("operation rolled back"))
	}

	logger.Log("✍️", "Updating manifest...")
	updateManifest(m, changes)

	if err := manifest.Save(m); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Manifest save failed: %v\n", err)
		return rollbackAndWrapError(savepointTag, errors.New("manifest save failed, operation rolled back"))
	}

	logger.Log("💾", "Committing changes...")
	if err := git.CommitChanges(commitMessage); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ Commit failed: %v\n", err)
		return rollbackAndWrapError(savepointTag, errors.New("commit failed, operation rolled back"))
	}

	logger.SetTotalSteps(langApplyTotalSteps)
	logger.Log("🧹", "Cleaning up savepoint...")
	if err := git.DeleteSavepoint(savepointTag); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to delete savepoint tag '%s'. You may want to remove it manually.\n", savepointTag)
	}

	return nil
}

// rollbackAndWrapError attempts a Git rollback and returns an appropriately wrapped error.
func rollbackAndWrapError(savepointTag string, originalErr error) error {
	fmt.Println("Rolling back changes...")
	if rollbackErr := git.RollbackToSavepoint(savepointTag); rollbackErr != nil {
		return fmt.Errorf("CRITICAL: Task failed AND rollback failed: %w. Git tag '%s' must be manually removed", rollbackErr, savepointTag)
	}
	return originalErr
}

// updateManifest merges new projects and templates into the existing manifest structure.
func updateManifest(m *types.Manifest, changes *manifestChanges) {
	// Update Projects (ensure no duplicates by path)
	existingProjects := make(map[string]bool)
	for _, proj := range m.Projects {
		existingProjects[proj.Path] = true
	}
	for _, newProj := range changes.Projects {
		if !existingProjects[newProj.Path] {
			m.Projects = append(m.Projects, newProj)
		}
	}
	// Update Templates (ensure no duplicates by name + path)
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
}

// buildPlan constructs the list of tasks based on CLI flags.
func buildPlan(rc RunContext) (*types.Plan, string, *manifestChanges, error) { // FIXED: cyclop (Reduced complexity)
	plan := &types.Plan{Tasks: []types.Task{}}
	changes := &manifestChanges{}
	commitMessage := "scbake: Apply templates"
	didSomething := false // Flag to ensure at least one action is requested

	if rc.LangFlag != "" {
		didSomething = true
		msg, err := handleLangFlag(rc, plan, changes)
		if err != nil {
			return nil, "", nil, err
		}
		commitMessage = msg
	}

	if len(rc.WithFlag) > 0 {
		didSomething = true
		if err := handleWithFlag(rc, plan, changes); err != nil {
			return nil, "", nil, err
		}
		// Update commit message based on what was done
		if rc.LangFlag == "" {
			commitMessage = fmt.Sprintf("scbake: Apply templates (%v) to %s", rc.WithFlag, rc.ManifestPathArg)
		}
	}

	// Only fail if neither a language nor tooling was specified.
	if !didSomething {
		return nil, "", nil, errors.New("no language or templates specified. Use --lang or --with")
	}

	return plan, commitMessage, changes, nil
}

// handleLangFlag processes the --lang flag, adding language tasks and project info.
func handleLangFlag(rc RunContext, plan *types.Plan, changes *manifestChanges) (string, error) { // Extracted for cyclop reduction
	// Binary check
	switch rc.LangFlag {
	case "go":
		if err := preflight.CheckBinaries("go"); err != nil {
			return "", err
		}
	case "svelte":
		if err := preflight.CheckBinaries("npm"); err != nil {
			return "", err
		}
	case "spring":
		if err := preflight.CheckBinaries("curl", "unzip", "java"); err != nil {
			return "", err
		}
	}

	handler, err := lang.GetHandler(rc.LangFlag)
	if err != nil {
		return "", err
	}

	langTasks, err := handler.GetTasks(rc.TargetPath)
	if err != nil {
		return "", fmt.Errorf("failed to get tasks for lang '%s': %w", rc.LangFlag, err)
	}

	plan.Tasks = append(plan.Tasks, langTasks...)

	// Sanitize the project name for the manifest
	projectName, err := util.SanitizeModuleName(rc.ManifestPathArg)
	if err != nil {
		return "", fmt.Errorf("could not determine project name: %w", err)
	}

	changes.Projects = append(changes.Projects, types.Project{
		Name:     projectName,
		Path:     rc.ManifestPathArg,
		Language: rc.LangFlag,
	})

	return fmt.Sprintf("scbake: Apply '%s' to %s", rc.LangFlag, rc.ManifestPathArg), nil
}

// handleWithFlag processes the --with flag, adding template tasks.
func handleWithFlag(rc RunContext, plan *types.Plan, changes *manifestChanges) error { // Extracted for cyclop reduction
	for _, tmplName := range rc.WithFlag {
		handler, err := templates.GetHandler(tmplName)
		if err != nil {
			return err
		}

		tmplTasks, err := handler.GetTasks(rc.TargetPath)
		if err != nil {
			return fmt.Errorf("failed to get tasks for template '%s': %w", tmplName, err)
		}

		plan.Tasks = append(plan.Tasks, tmplTasks...)
	}

	// Simplified change tracking for root templates:
	changes.Templates = append(changes.Templates, types.Template{
		Name: "root-templates",
		Path: rc.ManifestPathArg,
	})
	return nil
}
