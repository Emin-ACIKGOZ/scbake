// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package cmd implements the command-line commands for scbake.
package cmd

import (
	"fmt"
	"os"
	"scbake/internal/util/fileutil"

	"github.com/spf13/cobra"
)

// Set this with a linker flag during the build process in the future.
var version = "v0.0.1-dev"

var (
	dryRun bool
	force  bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "scbake",
	Short: "A manifest-driven project scaffolder",
	Long: `scbake is a single-binary CLI for scaffolding new projects
and applying layered infrastructure templates.`,

	// If the user just types 'scbake', show the version
	Run: func(cmd *cobra.Command, _ []string) {
		v, _ := cmd.Flags().GetBool("version")
		if v {
			fmt.Println(version)
			os.Exit(fileutil.ExitSuccess)
		}
		if err := cmd.Help(); err != nil {
			// This typically shouldn't fail, but check it for robustness.
			fmt.Fprintf(os.Stderr, "Error showing help: %v\n", err)
			os.Exit(fileutil.ExitError)
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(fileutil.ExitError)
	}
}

func init() {
	// Add persistent flags, available to all subcommands
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Show what changes would be made without executing them")
	rootCmd.PersistentFlags().BoolVar(&force, "force", false, "Override safety checks for file overwrites")

	// Add a local version flag to the root command
	rootCmd.Flags().BoolP("version", "v", false, "Show the scbake version")

	// Add subcommands
	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(listCmd)
}
