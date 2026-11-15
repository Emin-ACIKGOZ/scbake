// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"scbake/internal/templateregistry"
	"scbake/pkg/lang"
	"scbake/pkg/templates"

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
		if err := templateregistry.ValidateName(args[0]); err != nil {
			return fmt.Errorf("invalid registry name: %w", err)
		}
		m, err := templateregistry.NewManager()
		if err != nil {
			return err
		}
		if err := m.Add(args[0], args[1], registryAddToken, registryAddVersion, registryAddSubdirectory); err != nil {
			return err
		}
		msg := fmt.Sprintf("Added registry %q (%s)", args[0], templateregistry.RedactURL(args[1]))
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
			fmt.Printf("  %s (%s)%s\n", r.Name, templateregistry.RedactURL(r.URL), extra)
		}
		return nil
	},
}

var pullCmd = &cobra.Command{
	Use:   "pull <name> [--registry <registry>]",
	Short: "Pull a template from a registry",
	Long: `Downloads a template archive from a registry and caches it locally.
Only built-in tooling templates and language packs can be pulled: a registry
overrides files belonging to a name scbake already knows and cannot add new
names. Use 'scbake list templates' or 'scbake list langs' to see valid names.
Use --registry to specify which registry to pull from.
A pull makes a network request only when the cached copy is missing, invalid,
or recorded for a different version or source URL; when a valid cached copy
exists it is reported as up to date and no request is made. There is no --refresh flag; remove the cache to force a fetch.
Authentication uses the stored token or the SCBAKE_REGISTRY_TOKEN_<NAME> env var.
Pulled files are used as overrides by handlers that read template files; the
git, svelte, and spring handlers currently scaffold without overridable
templates.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		templateName := args[0]
		if err := templateregistry.ValidateName(templateName); err != nil {
			return fmt.Errorf("invalid template name: %w", err)
		}
		if _, err := templates.GetHandler(templateName); err != nil {
			if _, langErr := lang.GetHandler(templateName); langErr != nil {
				return fmt.Errorf(
					"cannot pull %q: registries only override built-in templates and language packs, they cannot add new names",
					templateName,
				)
			}
		}
		if pullRegistryName != "" {
			if err := templateregistry.ValidateName(pullRegistryName); err != nil {
				return fmt.Errorf("invalid registry name: %w", err)
			}
		}

		m, err := templateregistry.NewManager()
		if err != nil {
			return err
		}

		if pullRegistryName != "" {
			fetched, err := m.PullContext(cmd.Context(), pullRegistryName, templateName)
			if err != nil {
				return fmt.Errorf("pulling template %q from registry %q: %w", templateName, pullRegistryName, err)
			}
			printPullResult(templateName, pullRegistryName, fetched)
		} else {
			registries := m.List()
			if len(registries) == 0 {
				return errors.New("no registries configured; use 'scbake template registry add' first")
			}
			var lastErr error
			for _, r := range registries {
				fetched, err := m.PullContext(cmd.Context(), r.Name, templateName)
				if err != nil {
					lastErr = err
					continue
				}
				printPullResult(templateName, r.Name, fetched)
				return nil
			}
			return fmt.Errorf("could not pull %q from any registry: %v", templateName, lastErr)
		}

		return nil
	},
}

// printPullResult reports whether the template was downloaded or served from a
// valid cache entry, which makes no network request.
func printPullResult(templateName, registryName string, fetched bool) {
	if fetched {
		fmt.Printf("Pulled template %q from registry %q\n", templateName, registryName)
		return
	}
	fmt.Printf("Template %q from registry %q is up to date (cached)\n", templateName, registryName)
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
