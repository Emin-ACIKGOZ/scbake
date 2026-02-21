// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package cmd implements the command-line commands for scbake.
package cmd

import (
	"fmt"
	"os"
	"scbake/internal/core"
	"scbake/internal/util/fileutil"

	"github.com/spf13/cobra"
)

// Steps in the new command run
const newCmdTotalSteps = 4

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

		// Flag to track directory creation
		dirCreated := false

		// runNew takes a pointer to dirCreated to track creation status
		if err := runNew(projectName, &dirCreated); err != nil {
			// SAFETY CHECK: Only clean up the directory if we created it during this command.
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

// runNew takes a pointer to dirCreated to track creation status.
func runNew(projectName string, dirCreated *bool) error {
	logger := core.NewStepLogger(newCmdTotalSteps, dryRun)

	// Capture original working directory before any changes.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get cwd: %w", err)
	}

	// 1. Check if directory exists
	if _, err := os.Stat(projectName); !os.IsNotExist(err) {
		return fmt.Errorf("directory '%s' already exists", projectName)
	}

	// 2. Create directory
	logger.Log("📁", "Creating directory: "+projectName)
	if !dryRun {
		if err := os.Mkdir(projectName, fileutil.DirPerms); err != nil {
			return err
		}
		*dirCreated = true // Set flag: successfully created directory
	}

	// 3. CD into directory
	if !dryRun {
		if err := os.Chdir(projectName); err != nil {
			return err
		}

		// Defer a function to return to the original CWD
		defer func() {
			_ = os.Chdir(cwd)
		}()
	}

	// Bootstrap manifest so the engine can find the project root
	if !dryRun {
		if err := os.WriteFile(fileutil.ManifestFileName, []byte(""), fileutil.PrivateFilePerms); err != nil {
			return fmt.Errorf("failed to bootstrap manifest: %w", err)
		}
	}

	// 6. Run the 'apply' logic
	logger.Log("🚀", "Applying templates...")
	rc := core.RunContext{
		LangFlag:        newLangFlag,
		WithFlag:        newWithFlag,
		TargetPath:      ".",    // Now correctly relative to the new directory
		DryRun:          dryRun, // Use global flag
		Force:           force,  // Use global flag
		ManifestPathArg: ".",
	}

	// RunApply prints its own logs.
	if err := core.RunApply(rc); err != nil {
		return err
	}

	// Update the total steps for the logger, using the exported method.
	logger.SetTotalSteps(newCmdTotalSteps)
	logger.Log("✨", "Finalizing project...")
	return nil
}

func init() {
	// The rootCmd registration is handled in cmd/root.go init()
	newCmd.Flags().StringVar(&newLangFlag, "lang", "", "Language project pack to apply")
	newCmd.Flags().StringSliceVar(&newWithFlag, "with", []string{}, "Tooling template(s) to apply")
}
