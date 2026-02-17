// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package core contains the central execution and planning logic.
package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"
	"scbake/internal/preflight"
	"scbake/internal/types"
	"scbake/internal/util"
	"scbake/pkg/lang"
	"scbake/pkg/templates"
)

// Define constants for step logging and cyclomatic complexity reduction
const (
	runApplyTotalSteps  = 5 // Now 5 steps, as git steps are part of template logic
	langApplyTotalSteps = 5
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

// RunApply is the main logic for the 'apply' command, extracted.
func RunApply(rc RunContext) error {
	logger := NewStepLogger(runApplyTotalSteps, rc.DryRun)

	logger.Log("📖", "Loading manifest (scbake.toml)...")

	// 1. Root Discovery & Manifest Load
	m, rootPath, err := manifest.Load(rc.TargetPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", manifest.ManifestFileName, err)
	}

	// 2. Initialize Transaction Engine
	// This is the safety net. We defer Rollback() immediately.
	// If the program panics or returns an error at any point,
	// the filesystem is restored to its original state.
	// If we succeed, we call tx.Commit() explicitly at the end, which disables the rollback.
	var tx *transaction.Manager
	if !rc.DryRun {
		tx, err = transaction.New(rootPath)
		if err != nil {
			return fmt.Errorf("failed to initialize transaction manager: %w", err)
		}
		// SAFETY: The defer ensures atomicity.
		defer func() {
			if rErr := tx.Rollback(); rErr != nil {
				// We log this to stderr because we can't return it easily from defer
				// without named return parameters, and panic recovery is complex.
				// In a normal failure flow, Rollback is expected to succeed silently.
				fmt.Fprintf(os.Stderr, "⚠️  Transaction rollback warning: %v\n", rErr)
			}
		}()
	}

	logger.Log("📝", "Building execution plan...")

	// Deduplicate requested templates to ensure idempotency
	rc.WithFlag = deduplicateTemplates(rc.WithFlag)

	plan, _, changes, err := buildPlan(rc)
	if err != nil {
		return err
	}

	// Prepare task context
	// NOTE: shallow copy of manifest. Ideally safe as we append to slices creating new backing arrays
	// if capacity is exceeded, but 'm' is effectively read-only until updateManifest.
	futureManifest := *m
	futureManifest.Projects = append(futureManifest.Projects, changes.Projects...)
	futureManifest.Templates = append(futureManifest.Templates, changes.Templates...)
	tc := types.TaskContext{
		Ctx:        context.Background(),
		DryRun:     rc.DryRun,
		Manifest:   &futureManifest,
		TargetPath: rc.TargetPath,
		Force:      rc.Force,
		Tx:         tx,
	}

	if rc.DryRun {
		fmt.Println("DRY RUN: No changes will be made.")
		fmt.Println("Plan contains the following tasks:")
		return Execute(plan, tc)
	}

	// 3. Execute and Finalize
	// We pass the transaction and paths down.
	return executeAndFinalize(logger, plan, tc, m, changes, rootPath, tx)
}

// executeAndFinalize runs the plan, updates manifest, and commits the transaction.
func executeAndFinalize(
	logger *StepLogger,
	plan *types.Plan,
	tc types.TaskContext,
	m *types.Manifest,
	changes *manifestChanges,
	rootPath string,
	tx *transaction.Manager,
) error {
	logger.Log("🚀", "Executing plan...")

	// Run all tasks. They will auto-track changes via tc.Tx.
	if err := Execute(plan, tc); err != nil {
		return fmt.Errorf("task execution failed: %w", err)
	}

	logger.Log("✍️", "Updating manifest...")
	updateManifest(m, changes)

	// We track the manifest file itself before saving.
	// This ensures that if the Save succeeds but a subsequent step crashes (unlikely),
	// the manifest is rolled back to sync with the filesystem.
	manifestPath := filepath.Join(rootPath, manifest.ManifestFileName)
	if err := tx.Track(manifestPath); err != nil {
		return fmt.Errorf("failed to track manifest file: %w", err)
	}

	if err := manifest.Save(m, rootPath); err != nil {
		return fmt.Errorf("manifest save failed: %w", err)
	}

	logger.Log("✅", "Committing transaction...")
	// Point of No Return: We delete the backups.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
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

func deduplicateTemplates(requested []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, t := range requested {
		if !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	return result
}

// buildPlan constructs the list of tasks based on CLI flags.
func buildPlan(rc RunContext) (*types.Plan, string, *manifestChanges, error) {
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
func handleLangFlag(rc RunContext, plan *types.Plan, changes *manifestChanges) (string, error) {
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
func handleWithFlag(rc RunContext, plan *types.Plan, changes *manifestChanges) error {
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
