// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/manifest"
	"scbake/internal/util"
	"scbake/internal/util/fileutil"
	"scbake/pkg/lang"
	"scbake/pkg/templates"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list [langs|templates|projects]",
	Short: "List available resources or projects",
	Long: `Lists available language packs, tooling templates,
or the projects currently managed in this repository's ` + fileutil.ManifestFileName + `.`,
	Args: cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		switch args[0] {
		case "langs":
			fmt.Println("Available Language Packs:")
			for _, l := range lang.ListLangs() {
				fmt.Printf("  %s\n", l)
			}

		case "templates":
			fmt.Println("Available Tooling Templates:")
			for _, t := range templates.ListTemplates() {
				fmt.Printf("  %s\n", t)
			}

		case "projects":
			// Pass "." as start path; rootPath locates the manifest.
			m, rootPath, err := manifest.LoadWithOptions(".", manifest.DiscoveryOptions{
				AllowAncestorSearch: allowAncestorSearch,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading %s: %v\n", fileutil.ManifestFileName, err)
				os.Exit(fileutil.ExitError)
			}
			manifestPath := filepath.Join(rootPath, fileutil.ManifestFileName)
			if _, err := os.Stat(manifestPath); err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr,
						"Error: no %s found in %s; run 'scbake new' or 'scbake apply' first\n",
						fileutil.ManifestFileName, util.Sanitize(rootPath))
				} else {
					fmt.Fprintf(os.Stderr, "Error: cannot inspect %s: %v\n",
						util.Sanitize(manifestPath), util.SanitizeError(err))
				}
				os.Exit(fileutil.ExitError)
			}
			fmt.Printf("Managed Projects (from %s):\n", fileutil.ManifestFileName)
			if len(m.Projects) == 0 {
				fmt.Println("  No projects found.")
				return
			}
			for _, p := range m.Projects {
				fmt.Printf("  - %s (lang: %s, path: %s)\n",
					util.Sanitize(p.Name), util.Sanitize(p.Language), util.Sanitize(p.Path))
			}

		default:
			fmt.Fprintf(os.Stderr, "Error: Unknown resource type '%s'.\n", args[0])
			fmt.Println("Must be one of: langs, templates, projects")
			os.Exit(fileutil.ExitError)
		}
	},
}

// listCmd is registered on rootCmd in root.go's init to keep all
// subcommand registration in one place; no init is needed here.
