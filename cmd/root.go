package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// We'll set this with a linker flag during the build process later.
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
	Run: func(cmd *cobra.Command, args []string) {
		v, _ := cmd.Flags().GetBool("version")
		if v {
			fmt.Println(version)
			os.Exit(0)
		}
		cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
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
