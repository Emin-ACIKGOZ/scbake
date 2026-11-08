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
	"scbake/internal/drift"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/ui"
	"scbake/internal/util/fileutil"

	"github.com/spf13/cobra"
)

// errDriftDetected signals a non-zero exit for CI. It is returned after the
// findings have been printed; root.Execute maps any error to ExitError.
var errDriftDetected = errors.New("drift detected")

var (
	driftRepairFlag      bool
	driftStrategyFlag    string
	driftRequireBaseFlag bool
)

var driftCmd = &cobra.Command{
	Use:   "drift [path]",
	Short: "Detect local changes to managed files",
	Long: `Compares the managed_files hashes recorded in scbake.toml against the
working tree and the committed base under .scbake-base, and lists any file that
was modified or deleted since scbake wrote it and any base that is missing or no
longer matches its recorded hash. Detection always scans every managed file in
the manifest, regardless of <path>, and exits non-zero when blocking drift is
found, so it can gate a CI pipeline. A missing base is advisory by default; pass
--require-base to make it blocking. A missing scbake.toml is an error rather than
a clean result.

With --repair, the recorded templates are re-applied through the same
reconciliation used by 'update', restoring generated content while preserving
local edits according to --conflict-strategy. Because 'merge' preserves local
edits it does not clear drift; use 'overwrite' to force restoration. On a clean
tree, 'overwrite' also re-records a missing base; 'merge' does not backfill it.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		if driftRequireBaseFlag && driftRepairFlag {
			return errors.New("--require-base cannot be combined with --repair")
		}

		if driftRepairFlag {
			return repairDrift(cmd.Context(), absPath, manifestPathArg)
		}

		m, rootPath, err := manifest.LoadWithOptions(absPath, manifest.DiscoveryOptions{
			AllowAncestorSearch: allowAncestorSearch,
		})
		if err != nil {
			return fmt.Errorf("failed to load manifest: %w", err)
		}

		// LoadWithOptions deliberately returns an empty manifest when the file
		// is absent. For drift that would report a missing project as clean, so
		// fail closed at the discovered root instead.
		manifestPath := filepath.Join(rootPath, fileutil.ManifestFileName)
		if _, statErr := os.Stat(manifestPath); statErr != nil {
			if os.IsNotExist(statErr) {
				return fmt.Errorf("no %s found at %s", fileutil.ManifestFileName, rootPath)
			}
			return fmt.Errorf("failed to inspect %s: %w", manifestPath, statErr)
		}

		// An interrupted run leaves a journal that a plain drift scan cannot
		// see in managed_files. Never report such a tree as clean.
		if err := transaction.CheckPending(rootPath); err != nil {
			return err
		}

		findings, err := drift.Detect(m, rootPath)
		if err != nil {
			return err
		}

		if len(findings) == 0 {
			fmt.Println("✅ No drift detected.")
			return nil
		}

		blocking, advisory := partitionFindings(findings, driftRequireBaseFlag)
		if len(blocking) == 0 {
			fmt.Printf("No working-tree drift; %d merge base(s) missing (advisory):\n", len(advisory))
			printFindings(findings)
			fmt.Println("Run 'scbake drift --require-base' to make a missing base an error.")
			return nil
		}

		var working, invalid int
		for _, f := range blocking {
			switch f.Kind {
			case drift.Modified, drift.Missing:
				working++
			case drift.BaseInvalid:
				invalid++
			}
		}
		switch {
		case working > 0:
			fmt.Printf("Drift detected in %d managed file(s):\n", len(blocking))
		case invalid > 0:
			fmt.Printf("Invalid committed merge base for %d managed file(s):\n", invalid)
		default:
			fmt.Printf("Missing committed merge base for %d managed file(s):\n", len(blocking))
		}
		printFindings(findings)
		if len(advisory) > 0 {
			fmt.Printf("%d missing merge base(s) reported as advisory; use --require-base to fail on them.\n",
				len(advisory))
		}
		return errDriftDetected
	},
}

// partitionFindings splits findings into blocking and advisory. A missing
// committed base is advisory unless --require-base promotes it; every other
// kind, including base-invalid, always blocks.
func partitionFindings(findings []drift.Finding, requireBase bool) (blocking, advisory []drift.Finding) {
	for _, f := range findings {
		if f.Kind == drift.BaseMissing && !requireBase {
			advisory = append(advisory, f)
			continue
		}
		blocking = append(blocking, f)
	}
	return blocking, advisory
}

// printFindings prints the sorted finding list. Base findings include the
// expected committed base path and the recorded hash; an invalid base also
// shows the hash of the base on disk.
func printFindings(findings []drift.Finding) {
	for _, f := range findings {
		switch f.Kind {
		case drift.BaseMissing:
			fmt.Printf("  %-12s %s (expected base %s, recorded %s)\n",
				f.Kind, f.Path, f.BasePath, shortHash(f.Recorded))
		case drift.BaseInvalid:
			fmt.Printf("  %-12s %s (expected base %s, recorded %s, base %s)\n",
				f.Kind, f.Path, f.BasePath, shortHash(f.Recorded), shortHash(f.Actual))
		case drift.Missing:
			fmt.Printf("  %-12s %s (recorded %s)\n", f.Kind, f.Path, shortHash(f.Recorded))
		default:
			fmt.Printf("  %-12s %s (recorded %s, on disk %s)\n",
				f.Kind, f.Path, shortHash(f.Recorded), shortHash(f.Actual))
		}
	}
}

// repairDrift reuses 'update' so repair and update share one reconciliation and
// transaction implementation. Local edits are preserved per --conflict-strategy.
func repairDrift(ctx context.Context, absPath, manifestPathArg string) error {
	rc := core.RunContext{
		TargetPath:          absPath,
		ManifestPathArg:     manifestPathArg,
		DryRun:              dryRun,
		Force:               force,
		ConflictStrategy:    driftStrategyFlag,
		TemplateDir:         templateDirFlag,
		RegistryCacheDir:    GetRegistryCacheDir(),
		Ctx:                 ctx,
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
	driftCmd.Flags().BoolVar(
		&driftRequireBaseFlag,
		"require-base",
		false,
		"Exit non-zero when a committed merge base is missing",
	)
	driftCmd.Flags().StringVar(
		&driftStrategyFlag,
		"conflict-strategy",
		types.ConflictMerge,
		"Repair conflict strategy: merge, fail, overwrite, artifact, keep-local",
	)
}
