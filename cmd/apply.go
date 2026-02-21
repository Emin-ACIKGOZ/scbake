// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package cmd implements the command-line commands for scbake.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/core"

	"github.com/spf13/cobra"
)

var (
	langFlag string
	withFlag []string
)

var applyCmd = &cobra.Command{
	Use:   "apply [--lang <lang>] [--with <template...>] [<path>]",
	Short: "Apply a language pack or tooling template to a project",
	Long: `Applies language packs or tooling templates to a specified path.
This command is atomic and requires a clean Git working tree.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		// Store the original argument for the manifest, which must be relative.
		manifestPathArg := "."
		targetPath := "."
		if len(args) > 0 {
			manifestPathArg = args[0]
			targetPath = args[0]
		}

		// Convert to absolute path for robust execution (npm, go build).
		absPath, err := filepath.Abs(targetPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving path %s: %v\n", targetPath, err)
			os.Exit(1)
		}

		rc := core.RunContext{
			LangFlag:        langFlag,
			WithFlag:        withFlag,
			TargetPath:      absPath,         // Pass absolute path for execution stability.
			ManifestPathArg: manifestPathArg, // Pass Arg for manifest portability.
			DryRun:          dryRun,          // dryRun is the global flag.
			Force:           force,           // force is the global flag.
		}

		if err := core.RunApply(rc); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Success! 'apply' command finished.")
	},
}

func init() {
	rootCmd.AddCommand(applyCmd)
	applyCmd.PersistentFlags().StringVar(&langFlag, "lang", "", "Language project pack to apply (e.g., 'go')")
	applyCmd.PersistentFlags().StringSliceVar(&withFlag, "with", []string{}, "Tooling template to apply (e.g., 'makefile')")
}
