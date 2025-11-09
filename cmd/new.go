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
	Use:   "new <project-name> [--lang <lang>] [--with <template...>]",
	Short: "Create a new standalone project",
	Long: `Creates a new directory, initializes a Git repository,
and applies the specified language pack and templates.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		projectName := args[0]

		// Flag to track directory creation
		dirCreated := false

		// Get the original working directory *before* we do anything.
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// runNew takes a pointer to dirCreated to track creation status
		if err := runNew(projectName, &dirCreated); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)

			// Go back to the original directory
			os.Chdir(cwd)

			// SAFETY CHECK: Only clean up the directory if we created it during this command.
			if dirCreated {
				fmt.Fprintf(os.Stderr, "Cleaning up %s...\n", projectName)
				os.RemoveAll(projectName)
			}
			os.Exit(1)
		}
		fmt.Printf("✅ Success! New project '%s' created.\n", projectName)
	},
}

// runNew takes a pointer to dirCreated to track creation status.
func runNew(projectName string, dirCreated *bool) error {
	logger := core.NewStepLogger(6, dryRun)

	// 1. Check if directory exists
	if _, err := os.Stat(projectName); !os.IsNotExist(err) {
		return fmt.Errorf("directory '%s' already exists", projectName)
	}

	// 2. Create directory
	logger.Log("📁", "Creating directory: "+projectName)
	if err := os.Mkdir(projectName, 0755); err != nil {
		return err
	}
	*dirCreated = true // Set flag: successfully created directory

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

	// RunApply prints its own logs.
	if err := core.RunApply(rc); err != nil {
		return err
	}

	// Update the total steps for the logger, using the exported method.
	logger.SetTotalSteps(6)
	logger.Log("✨", "Finalizing project...")
	return nil
}

func init() {
	// The rootCmd registration is handled in cmd/root.go init()
	newCmd.Flags().StringVar(&newLangFlag, "lang", "", "Language project pack to apply (e.g., 'go')")
	newCmd.Flags().StringSliceVar(&newWithFlag, "with", []string{}, "Tooling template(s) to apply")
}
