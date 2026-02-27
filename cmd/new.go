// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package cmd

import (
	"fmt"
	"os"
	"scbake/internal/core"
	"scbake/internal/ui"
	"scbake/internal/util/fileutil"

	"github.com/spf13/cobra"
)

// newCmdTotalSteps represents the total milestones in the 'new' workflow:
// 1. Directory creation
// 2. Template application start
// 3-7. Internal RunApply milestones (Load, Plan, Execute, Manifest, Commit)
// 8. Finalization
const newCmdTotalSteps = 8

var (
	newLangFlag string
	newWithFlag []string
)

var newCmd = &cobra.Command{
	Use:   "new <project-name> [--lang <lang>] [--with <template...>]",
	Short: "Create a new standalone project",
	Long:  `Creates a new directory and applies the specified language pack and templates.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		projectName := args[0]
		dirCreated := false

		if err := runNew(projectName, &dirCreated); err != nil {
			// Cleanup: Only remove the directory if it was created during this session.
			if dirCreated {
				fmt.Fprintf(os.Stderr, "Cleaning up failed project directory '%s'...\n", projectName)
				_ = os.RemoveAll(projectName)
			}
			return err
		}

		fmt.Printf("✅ Success! New project '%s' created.\n", projectName)
		return nil
	},
}

// runNew coordinates the project directory setup and delegates template application to the core engine.
func runNew(projectName string, dirCreated *bool) error {
	reporter := ui.NewReporter(newCmdTotalSteps, dryRun)

	// Capture original working directory before any changes.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get cwd: %w", err)
	}

	// Verify target directory availability
	if _, err := os.Stat(projectName); !os.IsNotExist(err) {
		return fmt.Errorf("directory '%s' already exists", projectName)
	}

	// Initialize project directory
	reporter.Step("📁", "Creating directory: "+projectName)
	if !dryRun {
		if err := os.Mkdir(projectName, fileutil.DirPerms); err != nil {
			return err
		}
		*dirCreated = true
	}

	// Relocate to target directory for atomic scaffolding
	if !dryRun {
		if err := os.Chdir(projectName); err != nil {
			return err
		}

		// Defer a function to return to the original CWD
		defer func() {
			_ = os.Chdir(cwd)
		}()
	}

	// Bootstrap manifest for project root discovery
	if !dryRun {
		if err := os.WriteFile(fileutil.ManifestFileName, []byte(""), fileutil.PrivateFilePerms); err != nil {
			return fmt.Errorf("failed to bootstrap manifest: %w", err)
		}
	}

	// Delegate template and language pack application to the core executor
	reporter.Step("🚀", "Applying templates...")
	rc := core.RunContext{
		LangFlag:        newLangFlag,
		WithFlag:        newWithFlag,
		TargetPath:      ".",
		DryRun:          dryRun,
		Force:           force,
		ManifestPathArg: ".",
	}

	if err := core.RunApply(rc, reporter); err != nil {
		return err
	}

	// Finalize output
	reporter.Step("✨", "Finalizing project...")
	return nil
}

func init() {
	// The rootCmd registration is handled in cmd/root.go init()
	newCmd.Flags().StringVar(&newLangFlag, "lang", "", "Language project pack to apply")
	newCmd.Flags().StringSliceVar(&newWithFlag, "with", []string{}, "Tooling template(s) to apply")
}
