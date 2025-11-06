package cmd

import (
	"fmt"
	"os"
	"scbake/internal/manifest"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list [langs|templates|projects]",
	Short: "List available resources or projects",
	Long: `Lists available language packs, tooling templates,
or the projects currently managed in this repository's scbake.toml.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "langs":
			fmt.Println("Available Language Packs:")
			fmt.Println("  go")
			// Add more as they are implemented

		case "templates":
			fmt.Println("Available Tooling Templates:")
			fmt.Println("  makefile")
			// Add more as they are implemented

		case "projects":
			fmt.Println("Managed Projects (from scbake.toml):")
			m, err := manifest.Load()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading scbake.toml: %v\n", err)
				os.Exit(1)
			}
			if len(m.Projects) == 0 {
				fmt.Println("  No projects found.")
				return
			}
			for _, p := range m.Projects {
				fmt.Printf("  - %s (lang: %s, path: %s)\n", p.Name, p.Language, p.Path)
			}

		default:
			fmt.Fprintf(os.Stderr, "Error: Unknown resource type '%s'.\n", args[0])
			fmt.Println("Must be one of: langs, templates, projects")
			os.Exit(1)
		}
	},
}

func init() {
	// This was already done in commit 1
	// rootCmd.AddCommand(listCmd)
}
