// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"path/filepath"
	"scbake/internal/core"
	"scbake/internal/types"
	"scbake/internal/ui"

	"github.com/spf13/cobra"
)

// runUpdateTotalSteps mirrors the milestones of an update run: load manifest,
// plan, execute, persist manifest, commit.
const runUpdateTotalSteps = 5

var updateConflictStrategyFlag string

var updateCmd = &cobra.Command{
	Use:   "update [path]",
	Short: "Update a generated project from its recorded templates",
	Long: `Re-applies the language pack and tooling templates recorded in scbake.toml,
bringing a generated project up to date without clobbering local edits.

With no template change the command is a no-op, so it is safe to run repeatedly.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		manifestPathArg := "."
		targetPath := "."
		if len(args) > 0 {
			manifestPathArg = args[0]
			targetPath = args[0]
		}

		absPath, err := filepath.Abs(targetPath)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		rc := core.RunContext{
			TargetPath:       absPath,
			ManifestPathArg:  manifestPathArg,
			DryRun:           dryRun,
			Force:            force,
			ConflictStrategy: updateConflictStrategyFlag,
			TemplateDir:      templateDirFlag,
			RegistryCacheDir: GetRegistryCacheDir(),
		}

		reporter := ui.NewReporter(runUpdateTotalSteps, dryRun)
		if err := core.RunUpdate(rc, reporter); err != nil {
			return err
		}
		fmt.Println("✅ Success! 'update' command finished.")
		return nil
	},
}

func init() {
	updateCmd.Flags().StringVar(
		&updateConflictStrategyFlag,
		"conflict-strategy",
		types.ConflictMerge,
		"Conflict resolution strategy: merge, fail, overwrite, artifact, keep-local",
	)
}
