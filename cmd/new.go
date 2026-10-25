// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"scbake/internal/core"
	"scbake/internal/templateregistry"
	"scbake/internal/types"
	"scbake/internal/ui"
	"scbake/internal/util/fileutil"
	"strings"

	"github.com/spf13/cobra"
)

// newCmdTotalSteps represents the total milestones in the 'new' workflow:
// 1. Directory creation
// 2. Template application start
// 3-7. Internal RunApply milestones (Load, Plan, Execute, Manifest, Commit)
// 8. Finalization
const newCmdTotalSteps = 8

var (
	newLangFlag             string
	newWithFlag             []string
	newLicenseFlag          string
	newCopyrightHolderFlag  string
	newConflictStrategyFlag string
	newSetFlag              []string
)

var newCmd = &cobra.Command{
	Use:   "new <project-name> [--lang <lang>] [--with <template...>]",
	Short: "Create a new standalone project",
	Long:  `Creates a new directory and applies the specified language pack and templates.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		projectName := args[0]
		dirCreated := false

		if err := runNew(projectName, &dirCreated); err != nil {
			// Cleanup: Only remove the directory if it was created during this session.
			if dirCreated {
				fmt.Fprintf(os.Stderr, "Cleaning up failed project directory '%s'...\n", projectName)
				_ = os.RemoveAll(projectName)
			}
			return err
		}

		fmt.Printf("✅ Success! New project '%s' created.\n", projectName)
		return nil
	},
}

// runNew coordinates the project directory setup and delegates template application to the core engine.
func runNew(projectName string, dirCreated *bool) error {
	// Reject traversal or absolute project names before touching the filesystem
	// so 'new' can never create a directory outside the current working dir.
	if err := templateregistry.ValidateName(projectName); err != nil {
		return fmt.Errorf("invalid project name: %w", err)
	}

	// Validate every flag before mutating the filesystem (SCB-012): a malformed
	// --set pair, an unknown --set key, or a missing --lang/--with must fail
	// before the project directory or its manifest is created, so a failed 'new'
	// leaves no residue behind. This mirrors the ordering already used by
	// 'apply' (see cmd/apply.go).
	setVars, err := parseSetFlags(newSetFlag)
	if err != nil {
		return err
	}
	if err := core.ValidateSetKeys(newWithFlag, newLangFlag, setVars); err != nil {
		return err
	}
	if newLangFlag == "" && len(newWithFlag) == 0 {
		return core.ErrNoSelection
	}

	reporter := ui.NewReporter(newCmdTotalSteps, dryRun)

	restoreWD, err := bootstrapProjectDir(projectName, dryRun, reporter, dirCreated)
	if err != nil {
		return err
	}
	defer restoreWD()

	// Delegate template and language pack application to the core executor
	reporter.Step("🚀", "Applying templates...")
	rc := core.RunContext{
		LangFlag:            newLangFlag,
		WithFlag:            newWithFlag,
		TargetPath:          ".",
		DryRun:              dryRun,
		Force:               force,
		ConflictStrategy:    newConflictStrategyFlag,
		TemplateDir:         templateDirFlag,
		RegistryCacheDir:    GetRegistryCacheDir(),
		ManifestPathArg:     ".",
		License:             newLicenseFlag,
		CopyrightHolder:     newCopyrightHolderFlag,
		SetVars:             setVars,
		AllowAncestorSearch: allowAncestorSearch,
	}

	if err := core.RunApply(rc, reporter); err != nil {
		return err
	}

	// Finalize output
	reporter.Step("✨", "Finalizing project...")
	return nil
}

// bootstrapProjectDir creates the project directory, changes working directory, and writes an initial manifest.
// Returns a cleanup closure that restores the original working directory upon completion.
func bootstrapProjectDir(projectName string, dryRun bool, reporter types.Reporter, dirCreated *bool) (func(), error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get cwd: %w", err)
	}

	restoreWD := func() { _ = os.Chdir(cwd) }

	if _, err := os.Stat(projectName); !os.IsNotExist(err) {
		return nil, fmt.Errorf("directory '%s' already exists", projectName)
	}

	reporter.Step("📁", "Creating directory: "+projectName)
	if !dryRun {
		if err := os.Mkdir(projectName, fileutil.DirPerms); err != nil {
			return nil, err
		}
		*dirCreated = true

		if err := os.Chdir(projectName); err != nil {
			return nil, err
		}

		if err := os.WriteFile(fileutil.ManifestFileName, []byte(""), fileutil.PrivateFilePerms); err != nil {
			return restoreWD, fmt.Errorf("failed to bootstrap manifest: %w", err)
		}
	}

	return restoreWD, nil
}

const maxKeyValueParts = 2

// parseSetFlags converts --set key=value entries into a map.
func parseSetFlags(flags []string) (map[string]string, error) {
	setVars := make(map[string]string, len(flags))
	for _, kv := range flags {
		parts := strings.SplitN(kv, "=", maxKeyValueParts)
		if len(parts) != maxKeyValueParts || parts[0] == "" {
			return nil, fmt.Errorf("invalid --set value %q: expected key=value", kv)
		}
		setVars[parts[0]] = parts[1]
	}
	return setVars, nil
}

func init() {
	// The rootCmd registration is handled in cmd/root.go init()
	newCmd.Flags().StringVar(&newLangFlag, "lang", "", "Language project pack to apply")
	newCmd.Flags().StringSliceVar(&newWithFlag, "with", []string{}, "Tooling template(s) to apply")
	newCmd.Flags().StringArrayVar(&newSetFlag, "set", []string{}, "Set template variable (key=value, can be repeated)")
	newCmd.Flags().StringVar(&newConflictStrategyFlag, "conflict-strategy", "fail", "Conflict resolution strategy: fail, overwrite, artifact, keep-local")
	newCmd.Flags().StringVar(&newLicenseFlag, "license", "", "SPDX License ID (required for compliance)")
	newCmd.Flags().StringVar(&newCopyrightHolderFlag, "copyright-holder", "", "Copyright holder name (required for compliance)")
}
