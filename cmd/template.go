// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package cmd

import (
	"errors"
	"fmt"
	"scbake/internal/templateregistry"

	"github.com/spf13/cobra"
)

var registryCacheDir string

// templateCmd represents the 'scbake template' command tree.
var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage template registries and remote templates",
	Long: `Manage remote template registries and pull templates.
Registries are named sources of template overrides that supplement
the embedded templates and --template-dir overrides.`,
}

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage template registries",
}

var (
	registryAddToken        string
	registryAddVersion      string
	registryAddSubdirectory string
)

var registryAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a template registry",
	Long: `Add a named template registry with optional authentication token,
default version, and subdirectory for template extraction.

Token can also be provided via SCBAKE_REGISTRY_TOKEN_<NAME> env var at pull time.
The --token flag stores the token in the config file; use the env var to avoid that.`,
	Args: cobra.ExactArgs(2), //nolint:mnd // 2 is the expected arg count for name+url
	RunE: func(_ *cobra.Command, args []string) error {
		m, err := templateregistry.NewManager()
		if err != nil {
			return err
		}
		if err := m.Add(args[0], args[1], registryAddToken, registryAddVersion, registryAddSubdirectory); err != nil {
			return err
		}
		msg := fmt.Sprintf("Added registry %q (%s)", args[0], args[1])
		if registryAddVersion != "" {
			msg += fmt.Sprintf(" [version: %s]", registryAddVersion)
		}
		if registryAddSubdirectory != "" {
			msg += fmt.Sprintf(" [subdir: %s]", registryAddSubdirectory)
		}
		fmt.Println(msg)
		return nil
	},
}

var registryRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a template registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		m, err := templateregistry.NewManager()
		if err != nil {
			return err
		}
		if err := m.Remove(args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed registry %q\n", args[0])
		return nil
	},
}

var registryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all template registries",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		m, err := templateregistry.NewManager()
		if err != nil {
			return err
		}
		registries := m.List()
		if len(registries) == 0 {
			fmt.Println("No registries configured.")
			return nil
		}
		fmt.Println("Template Registries:")
		for _, r := range registries {
			extra := ""
			if r.Version != "" {
				extra += " version=" + r.Version
			}
			if r.Subdirectory != "" {
				extra += " subdir=" + r.Subdirectory
			}
			if r.Token != "" {
				extra += " [token set]"
			}
			fmt.Printf("  %s (%s)%s\n", r.Name, r.URL, extra)
		}
		return nil
	},
}

var pullCmd = &cobra.Command{
	Use:   "pull <name> [--registry <registry>]",
	Short: "Pull a template from a registry",
	Long: `Downloads a template archive from a registry and caches it locally.
Use --registry to specify which registry to pull from.
Authentication uses the stored token or the SCBAKE_REGISTRY_TOKEN_<NAME> env var.
The template is then available as an override for scbake new/apply.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		m, err := templateregistry.NewManager()
		if err != nil {
			return err
		}

		templateName := args[0]
		if pullRegistryName != "" {
			if err := m.Pull(pullRegistryName, templateName); err != nil {
				return fmt.Errorf("pulling template %q from registry %q: %w", templateName, pullRegistryName, err)
			}
			fmt.Printf("Pulled template %q from registry %q\n", templateName, pullRegistryName)
		} else {
			registries := m.List()
			if len(registries) == 0 {
				return errors.New("no registries configured; use 'scbake template registry add' first")
			}
			var lastErr error
			for _, r := range registries {
				if err := m.Pull(r.Name, templateName); err != nil {
					lastErr = err
					continue
				}
				fmt.Printf("Pulled template %q from registry %q\n", templateName, r.Name)
				return nil
			}
			return fmt.Errorf("could not pull %q from any registry: %v", templateName, lastErr)
		}

		return nil
	},
}

var pullRegistryName string

func init() {
	pullCmd.Flags().StringVarP(&pullRegistryName, "registry", "r", "", "Registry to pull from")
	registryAddCmd.Flags().StringVar(&registryAddToken, "token", "", "Authentication token for the registry")
	registryAddCmd.Flags().StringVar(&registryAddVersion, "version", "", "Default template version")
	registryAddCmd.Flags().StringVar(&registryAddSubdirectory, "subdirectory", "", "Subdirectory within template archives")

	registryCmd.AddCommand(registryAddCmd)
	registryCmd.AddCommand(registryRemoveCmd)
	registryCmd.AddCommand(registryListCmd)
	templateCmd.AddCommand(registryCmd)
	templateCmd.AddCommand(pullCmd)

	m, err := templateregistry.NewManager()
	if err == nil {
		registryCacheDir = m.CacheDir()
	}
	rootCmd.AddCommand(templateCmd)
}

// GetRegistryCacheDir returns the path to the local template cache.
func GetRegistryCacheDir() string {
	if registryCacheDir == "" {
		m, err := templateregistry.NewManager()
		if err == nil {
			registryCacheDir = m.CacheDir()
		}
	}
	return registryCacheDir
}
