// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"scbake/internal/core"
	"scbake/internal/drift"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/ui"

	"github.com/spf13/cobra"
)

// errDriftDetected signals a non-zero exit for CI. It is returned after the
// findings have been printed; root.Execute maps any error to ExitError.
var errDriftDetected = errors.New("managed files have drifted")

var (
	driftRepairFlag   bool
	driftStrategyFlag string
)

var driftCmd = &cobra.Command{
	Use:   "drift [path]",
	Short: "Detect local changes to managed files",
	Long: `Compares the managed_files hashes recorded in scbake.toml against the
working tree and lists any file that was modified or deleted since scbake wrote
it. Detection always scans every managed file in the manifest, regardless of
<path>, and exits non-zero when drift is found, so it can gate a CI pipeline.

With --repair, the recorded templates are re-applied through the same
reconciliation used by 'update', restoring generated content while preserving
local edits according to --conflict-strategy. Because 'merge' preserves local
edits it does not clear drift; use 'overwrite' to force restoration.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, args []string) error {
		manifestPathArg := "."
		targetPath := "."
		if len(args) > 0 {
			manifestPathArg = args[0]
			targetPath = args[0]
		}

		absPath, err := filepath.Abs(targetPath)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		if driftRepairFlag {
			return repairDrift(absPath, manifestPathArg)
		}

		m, rootPath, err := manifest.LoadWithOptions(absPath, manifest.DiscoveryOptions{
			AllowAncestorSearch: allowAncestorSearch,
		})
		if err != nil {
			return fmt.Errorf("failed to load manifest: %w", err)
		}

		findings, err := drift.Detect(m, rootPath)
		if err != nil {
			return err
		}

		if len(findings) == 0 {
			fmt.Println("✅ No drift detected.")
			return nil
		}

		fmt.Printf("Drift detected in %d managed file(s):\n", len(findings))
		for _, f := range findings {
			if f.Kind == drift.Missing {
				fmt.Printf("  %-9s %s (recorded %s)\n", f.Kind, f.Path, shortHash(f.Recorded))
				continue
			}
			fmt.Printf("  %-9s %s (recorded %s, on disk %s)\n",
				f.Kind, f.Path, shortHash(f.Recorded), shortHash(f.Actual))
		}
		return errDriftDetected
	},
}

// repairDrift reuses 'update' so repair and update share one reconciliation and
// transaction implementation. Local edits are preserved per --conflict-strategy.
func repairDrift(absPath, manifestPathArg string) error {
	rc := core.RunContext{
		TargetPath:          absPath,
		ManifestPathArg:     manifestPathArg,
		DryRun:              dryRun,
		Force:               force,
		ConflictStrategy:    driftStrategyFlag,
		TemplateDir:         templateDirFlag,
		RegistryCacheDir:    GetRegistryCacheDir(),
		AllowAncestorSearch: allowAncestorSearch,
	}
	reporter := ui.NewReporter(runUpdateTotalSteps, dryRun)
	return core.RunUpdate(rc, reporter)
}

// shortHash abbreviates a SHA-256 for human-readable output; the full hash is
// not needed for a CI gate.
func shortHash(h string) string {
	const n = 12
	if len(h) > n {
		return h[:n]
	}
	return h
}

func init() {
	driftCmd.Flags().BoolVar(&driftRepairFlag, "repair", false, "Re-apply recorded templates to repair drift")
	driftCmd.Flags().StringVar(
		&driftStrategyFlag,
		"conflict-strategy",
		types.ConflictMerge,
		"Repair conflict strategy: merge, fail, overwrite, artifact, keep-local",
	)
}
