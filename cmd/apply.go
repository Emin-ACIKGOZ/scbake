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

const (
	runApplyTotalSteps = 5 // Now 5 steps, as git steps are part of template logic
)

var (
	langFlag             string
	withFlag             []string
	licenseFlag          string
	copyrightHolderFlag  string
	conflictStrategyFlag string
	applySetFlag         []string
)

var applyCmd = &cobra.Command{
	Use:   "apply [--lang <lang>] [--with <template...>] [<path>]",
	Short: "Apply a language pack or tooling template to a project",
	Args:  cobra.MaximumNArgs(1),
	PreRunE: func(*cobra.Command, []string) error {
		return types.ValidateConflictStrategy(conflictStrategyFlag)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Store the original argument for the manifest, which must be relative.
		manifestPathArg := "."
		targetPath := "."
		if len(args) > 0 {
			manifestPathArg = args[0]
			targetPath = args[0]
		}

		// Parse --set key=value pairs
		setVars, err := parseSetFlags(applySetFlag)
		if err != nil {
			return err
		}

		// Reject --set keys not declared by any selected schema before running.
		if err := core.ValidateSetKeys(withFlag, langFlag, setVars); err != nil {
			return err
		}

		// Convert to absolute path for robust execution (npm, go build).
		absPath, err := filepath.Abs(targetPath)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		resolvedTemplateDir, err := resolveTemplateDir(templateDirFlag)
		if err != nil {
			return err
		}

		cacheDir, registryNames := registryPolicy()
		rc := core.RunContext{
			LangFlag:            langFlag,
			WithFlag:            withFlag,
			TargetPath:          absPath,         // Resolved here for stable execution; relative values resolve against cwd.
			ManifestPathArg:     manifestPathArg, // Raw argument for commit text only.
			DryRun:              dryRun,          // dryRun is the global flag.
			Force:               force,           // force is the global flag.
			ConflictStrategy:    conflictStrategyFlag,
			TemplateDir:         resolvedTemplateDir,
			RegistryCacheDir:    cacheDir,
			RegistryNames:       registryNames,
			License:             licenseFlag,
			CopyrightHolder:     copyrightHolderFlag,
			SetVars:             setVars,
			Ctx:                 cmd.Context(),
			AllowAncestorSearch: allowAncestorSearch,
		}

		// Initialize modular UI reporter using the factory
		reporter := ui.NewReporter(runApplyTotalSteps, dryRun)

		if err := core.RunApply(rc, reporter); err != nil {
			return err
		}
		if dryRun {
			fmt.Println("✅ Dry run complete; no changes written.")
			return nil
		}
		fmt.Println("✅ Success! 'apply' command finished.")
		return nil
	},
}

func init() {
	// applyCmd is registered on rootCmd in root.go's init to keep all
	// subcommand registration in one place.
	applyCmd.PersistentFlags().StringVar(&langFlag, "lang", "", "Language pack")
	applyCmd.PersistentFlags().StringSliceVar(&withFlag, "with", []string{}, "Tooling templates")
	applyCmd.PersistentFlags().StringArrayVar(&applySetFlag, "set", []string{}, "Set template variable (key=value, can be repeated)")
	applyCmd.PersistentFlags().StringVar(&conflictStrategyFlag, "conflict-strategy", "fail", "Conflict resolution strategy: merge, fail, overwrite, artifact, keep-local")
	applyCmd.PersistentFlags().StringVar(&licenseFlag, "license", "", "SPDX License ID")
	applyCmd.PersistentFlags().StringVar(&copyrightHolderFlag, "copyright-holder", "", "Copyright holder name")
}
