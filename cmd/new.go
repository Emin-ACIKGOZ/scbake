package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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

		// Get the original working directory *before* we do anything.
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if err := runNew(projectName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			// Go back to the original directory *before* trying to clean up.
			os.Chdir(cwd)
			fmt.Fprintf(os.Stderr, "Cleaning up %s...\n", projectName)
			os.RemoveAll(projectName)
			os.Exit(1)
		}
		fmt.Printf("✅ Success! New project '%s' created.\n", projectName)
	},
}

func runNew(projectName string) error {
	// We now have 6 steps
	logger := core.NewStepLogger(6, dryRun)

	// 1. Check if directory exists
	if _, err := os.Stat(projectName); !os.IsNotExist(err) {
		return fmt.Errorf("directory '%s' already exists", projectName)
	}

	// 2. Create directory
	logger.Log("📁", fmt.Sprintf("Creating directory: %s", projectName))
	if err := os.Mkdir(projectName, 0755); err != nil {
		return err
	}

	// 3. CD into directory
	if err := os.Chdir(projectName); err != nil {
		return err
	}
	// Get the CWD inside the new dir
	cwd, _ := os.Getwd()
	// Defer a function to return to the original CWD (which is the parent)
	defer os.Chdir(filepath.Dir(cwd))

	// 4. Init Git
	logger.Log("GIT", "Initializing Git repository...")
	if err := git.CheckGitInstalled(); err != nil {
		return err
	}
	if err := git.Init(); err != nil {
		return err
	}

	// 5. Create initial empty commit to make HEAD valid
	logger.Log("GIT", "Creating initial commit...")
	if err := git.InitialCommit("scbake: Initial commit"); err != nil {
		return err
	}

	// 6. Run the 'apply' logic
	logger.Log("🚀", "Applying templates...")
	rc := core.RunContext{
		LangFlag:   newLangFlag,
		WithFlag:   newWithFlag,
		TargetPath: ".",
		DryRun:     dryRun, // Use global flag
		Force:      force,  // Use global flag
	}

	// RunApply will print its own logs, which is fine.
	if err := core.RunApply(rc); err != nil {
		return err
	}

	// We update the total steps for the logger
	logger.SetTotalSteps(6) // Use the exported method
	logger.Log("✨", "Finalizing project...")
	return nil
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().StringVar(&newLangFlag, "lang", "", "Language project pack to apply (required)")
	newCmd.Flags().StringSliceVar(&newWithFlag, "with", []string{}, "Tooling template(s) to apply")
}
