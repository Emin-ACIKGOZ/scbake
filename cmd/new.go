package cmd

import (
	"fmt"
	"os"
	"scbake/internal/core"
	"scbake/internal/git"

	"github.com/spf13/cobra"
)

var (
	newLangFlag string
	newWithFlag []string
)

var newCmd = &cobra.Command{
	Use:   "new <project-name> --lang <lang> [--with <template...>]",
	Short: "Create a new standalone project",
	Long: `Creates a new directory, initializes a Git repository,
and applies the specified language pack and templates.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		projectName := args[0]

		if newLangFlag == "" {
			fmt.Fprintln(os.Stderr, "Error: the --lang flag is required for 'new'")
			os.Exit(1)
		}

		if err := runNew(projectName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			// Attempt to clean up the failed directory
			fmt.Fprintf(os.Stderr, "Cleaning up %s...\n", projectName)
			os.RemoveAll(projectName)
			os.Exit(1)
		}
		fmt.Printf("✅ Success! New project '%s' created.\n", projectName)
	},
}

func runNew(projectName string) error {
	// 1. Check if directory exists
	if _, err := os.Stat(projectName); !os.IsNotExist(err) {
		return fmt.Errorf("directory '%s' already exists", projectName)
	}

	// 2. Create directory
	fmt.Printf("[1/4] 📁 Creating directory: %s\n", projectName)
	if err := os.Mkdir(projectName, 0755); err != nil {
		return err
	}

	// 3. CD into directory
	if err := os.Chdir(projectName); err != nil {
		return err
	}

	// 4. Init Git
	fmt.Println("[2/4] GIT Initializing Git repository...")
	if err := git.CheckGitInstalled(); err != nil {
		return err
	}
	if err := git.Init(); err != nil {
		return err
	}

	// 5. Run the 'apply' logic
	fmt.Println("[3/4] 🚀 Applying templates...")
	rc := core.RunContext{
		LangFlag:   newLangFlag,
		WithFlag:   newWithFlag,
		TargetPath: ".",
		DryRun:     dryRun, // Use global flag
	}

	// Note: We're not in an atomic transaction yet.
	// The 'apply' logic will create its *first* commit.
	if err := core.RunApply(rc); err != nil {
		return err
	}

	fmt.Println("[4/4] ✨ Finalizing project...")
	return nil
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().StringVar(&newLangFlag, "lang", "", "Language project pack to apply (required)")
	newCmd.Flags().StringSliceVar(&newWithFlag, "with", []string{}, "Tooling template(s) to apply")
}
