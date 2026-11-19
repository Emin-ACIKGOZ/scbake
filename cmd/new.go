// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/core"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/templateregistry"
	"scbake/internal/types"
	"scbake/internal/ui"
	"scbake/internal/util"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		projectName := args[0]
		dirCreated := false

		if err := runNew(cmd.Context(), projectName, &dirCreated); err != nil {
			// Cleanup: Only remove the directory if it was created during this
			// session. cleanupFailedProject re-acquires the root lock and skips
			// deletion when another process is using the directory.
			if dirCreated {
				cleanupFailedProject(projectName)
			}
			return err
		}

		if dryRun {
			fmt.Println("✅ Dry run complete; no changes written.")
			return nil
		}
		fmt.Printf("✅ Success! New project '%s' created.\n", projectName)
		return nil
	},
}

// cleanupFailedProject removes a project directory created by this invocation.
// It re-acquires the root lock first so a failed run cannot delete a directory
// another scbake process has started using. If locking is unavailable for a
// reason other than busy, cleanup proceeds as before.
func cleanupFailedProject(projectName string) {
	lock, err := transaction.AcquireRootLock(projectName, false)
	if err == nil {
		defer func() { _ = lock.Release() }()
		fmt.Fprintf(os.Stderr, "Cleaning up failed project directory '%s'...\n", projectName)
		if err := removeFailedProject(projectName, lock.Release); err != nil {
			fmt.Fprintf(os.Stderr, "Cleanup failed for '%s': %v\n", projectName, err)
		}
		return
	}
	if errors.Is(err, transaction.ErrBusy) {
		fmt.Fprintf(os.Stderr, "Not removing '%s': another scbake process is using it.\n", projectName)
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		return
	}

	fmt.Fprintf(os.Stderr, "Locking '%s' for cleanup failed: %v\n", projectName, err)
	fmt.Fprintf(os.Stderr, "Cleaning up failed project directory '%s'...\n", projectName)
	if removeErr := removeFailedProject(projectName, nil); removeErr != nil {
		fmt.Fprintf(os.Stderr, "Cleanup failed for '%s': %v\n", projectName, removeErr)
	}
}

// removeFailedProject mirrors removeBootstrappedRoot: create a unique sibling
// staging directory, release the lock, rename the directory into staging, then
// delete the staged tree. Windows cannot rename a directory containing an open
// lock file, so release must come first. If release or the rename fails, the
// original directory is left untouched. The release-before-rename race is
// accepted because the staging path is unique and cleanup only runs after a
// failed `new`.
func removeFailedProject(projectName string, release func() error) error {
	parent := filepath.Dir(projectName)
	staging, err := os.MkdirTemp(parent, ".scbake-remove-*")
	if err != nil {
		return fmt.Errorf("failed to stage failed project directory %s for removal: %w", projectName, err)
	}
	if release != nil {
		if err := release(); err != nil {
			_ = os.Remove(staging)
			return fmt.Errorf("failed to release failed project directory lock %s: %w", projectName, err)
		}
	}

	destination := filepath.Join(staging, filepath.Base(projectName))
	if err := os.Rename(projectName, destination); err != nil {
		_ = os.Remove(staging)
		return fmt.Errorf("failed to remove failed project directory %s: %w", projectName, err)
	}
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("removed failed project directory %s but failed to clean staging directory %s: %w", projectName, staging, err)
	}
	return nil
}

// validateNewTarget preserves the existing-directory precedence: an impossible
// target is reported before flag-specific preflight errors and before anything
// is created.
func validateNewTarget(projectName string) error {
	if newLangFlag == "" && len(newWithFlag) == 0 {
		return core.ErrNoSelection
	}
	if _, err := os.Stat(projectName); !os.IsNotExist(err) {
		return fmt.Errorf("directory '%s' already exists", util.Sanitize(projectName))
	}
	return nil
}

// validateNewSelection performs every flag and override-directory check that
// must fail before `new` mutates the filesystem (SCB-012). It returns the
// parsed --set values and the resolved template override directory.
func validateNewSelection(projectName string) (map[string]string, string, error) {
	setVars, err := parseSetFlags(newSetFlag)
	if err != nil {
		return nil, "", err
	}
	if err := core.ValidateSetKeys(newWithFlag, newLangFlag, setVars); err != nil {
		return nil, "", err
	}
	if err := validateNewTarget(projectName); err != nil {
		return nil, "", err
	}
	// The compliance template cannot generate LICENSE without both values.
	// Validate here, before bootstrapProjectDir creates the directory, so a
	// missing flag is an argument error rather than a late task failure that
	// has to clean up a partially written project.
	if err := validateComplianceSelection(setVars); err != nil {
		return nil, "", err
	}
	// Check the language toolchain before the directory exists, so a missing
	// binary (e.g. go) does not leave a directory to be cleaned up. A dry run
	// writes nothing, so it is left to core to report the missing tool as a
	// warning while the plan is still printed.
	if !dryRun {
		if err := core.CheckLangPrerequisites(newLangFlag); err != nil {
			return nil, "", err
		}
	}
	// Resolve the override directory before bootstrapping and chdir-ing into the
	// project, so a relative path is anchored at the invocation cwd and a stale
	// path fails instead of silently falling back to built-in templates.
	resolved, err := resolveTemplateDir(templateDirFlag)
	if err != nil {
		return nil, "", err
	}
	// The git template is planned after the directory exists, but checking here
	// means a missing identity fails before `new` writes anything at all,
	// including in a dry run: a preview that exits 0 for a run that cannot
	// commit is a false success. Probe the directory that will be created (its
	// nearest existing ancestor until then) rather than "." so validation and
	// the post-chdir apply share one skip/identity decision. An existing work
	// tree needs no identity because the template will be skipped.
	if err := validateGitSelection(projectName); err != nil {
		return nil, "", err
	}
	return setVars, resolved, nil
}

// validateGitSelection checks the git template's prerequisite identity before
// `new` writes anything, including in a dry run.
func validateGitSelection(projectName string) error {
	if !containsTemplate(newWithFlag, "git") {
		return nil
	}
	return core.CheckGitPrerequisites(projectName)
}

// newTargetPath returns the planning path for a new project. A real run has
// already changed into the project, so "." is its context; a dry run has not
// created the directory, so it plans against the relative project name.
func newTargetPath(projectName string) string {
	if dryRun {
		return projectName
	}
	return "."
}

// runNew coordinates the project directory setup and delegates template application to the core engine.
func runNew(ctx context.Context, projectName string, dirCreated *bool) error {
	// Reject traversal or absolute project names before touching the filesystem
	// so 'new' can never create a directory outside the current working dir.
	if err := templateregistry.ValidateName(projectName); err != nil {
		return fmt.Errorf("invalid project name: %w", err)
	}

	setVars, resolvedTemplateDir, err := validateNewSelection(projectName)
	if err != nil {
		return err
	}

	reporter := ui.NewReporter(newCmdTotalSteps, dryRun)

	restoreWD, err := bootstrapProjectDir(projectName, dryRun, reporter, dirCreated)
	if err != nil {
		return err
	}
	defer restoreWD()

	// Delegate template and language pack application to the core executor.
	// A real run has already changed into the new project, so "." is its
	// context. A dry run has not created or entered the directory, so plan
	// against the relative project name instead of the parent directory; core
	// resolves it against the working directory when it needs a canonical path.
	targetPath := newTargetPath(projectName)
	reporter.Step("🚀", "Applying templates...")
	cacheDir, registryNames := registryPolicy()
	rc := core.RunContext{
		LangFlag:            newLangFlag,
		WithFlag:            newWithFlag,
		TargetPath:          targetPath,
		DryRun:              dryRun,
		Force:               force,
		ConflictStrategy:    newConflictStrategyFlag,
		TemplateDir:         resolvedTemplateDir,
		RegistryCacheDir:    cacheDir,
		RegistryNames:       registryNames,
		ManifestPathArg:     ".",
		License:             newLicenseFlag,
		CopyrightHolder:     newCopyrightHolderFlag,
		SetVars:             setVars,
		Ctx:                 ctx,
		NewProject:          true,
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
//
// The empty manifest is written before RunApply acquires the root lock. A second
// `new` is stopped by the existence check, and concurrent apply in this
// brand-new directory is a narrow residual race; cleanup re-acquires the lock
// and refuses to stage the directory while another process is active.
func bootstrapProjectDir(projectName string, dryRun bool, reporter types.Reporter, dirCreated *bool) (func(), error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get cwd: %w", err)
	}

	restoreWD := func() { _ = os.Chdir(cwd) }

	if _, err := os.Stat(projectName); !os.IsNotExist(err) {
		return nil, fmt.Errorf("directory '%s' already exists", projectName)
	}

	step := "Creating directory: " + projectName
	if dryRun {
		step = "Would create directory: " + projectName
	}
	reporter.Step("📁", step)
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

// validateComplianceSelection checks the compliance template's required
// metadata before `new` creates the project directory.
func validateComplianceSelection(setVars map[string]string) error {
	if !containsTemplate(newWithFlag, "compliance") {
		return nil
	}
	license, copyrightHolder := core.ComplianceMetadata(newLicenseFlag, newCopyrightHolderFlag, setVars)
	return core.CheckCompliancePrerequisites(license, copyrightHolder)
}

// containsTemplate reports whether name appears in the template selection.
func containsTemplate(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

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
