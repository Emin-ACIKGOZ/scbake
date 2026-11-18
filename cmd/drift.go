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
		reportFindings(findings, blocking, advisory)
		if len(blocking) == 0 {
			return nil
		}
		return errDriftDetected
	},
}

// advisoryBaseSampleLimit bounds the per-file advisory output when many
// committed bases are missing at once. Detect still returns one finding per key
// for programmatic consumers; only the human summary is capped.
const advisoryBaseSampleLimit = 5

// findingCounts holds distinct affected path counts per category, so a path
// that yields both a working-tree and a base finding is counted once in each.
type findingCounts struct {
	working int
	invalid int
	missing int
}

// summarizeFindings counts distinct paths per finding category.
func summarizeFindings(findings []drift.Finding) findingCounts {
	working := make(map[string]struct{})
	invalid := make(map[string]struct{})
	missing := make(map[string]struct{})
	for _, f := range findings {
		switch f.Kind {
		case drift.Modified, drift.Missing:
			working[f.Path] = struct{}{}
		case drift.BaseInvalid:
			invalid[f.Path] = struct{}{}
		case drift.BaseMissing:
			missing[f.Path] = struct{}{}
		}
	}
	return findingCounts{working: len(working), invalid: len(invalid), missing: len(missing)}
}

// reportFindings prints the sorted detail list plus accurate category summaries.
// It does not decide the exit code.
func reportFindings(findings, blocking, advisory []drift.Finding) {
	blockingCounts := summarizeFindings(blocking)
	advisoryCounts := summarizeFindings(advisory)

	if len(blocking) == 0 {
		fmt.Printf("No working-tree drift; %d merge base(s) missing (advisory):\n",
			advisoryCounts.missing)
		printAdvisoryMissing(findings)
		fmt.Println("Run 'scbake drift --require-base' to make a missing base an error.")
		return
	}

	fmt.Printf("%s:\n", blockingHeader(blockingCounts))
	if len(advisory) > advisoryBaseSampleLimit {
		printFindings(blocking)
		printAdvisoryMissing(advisory)
	} else {
		printFindings(findings)
	}
	if advisoryCounts.missing > 0 {
		fmt.Printf("%d missing merge base(s) reported as advisory; use --require-base to fail on them.\n",
			advisoryCounts.missing)
	}
}

// blockingHeader describes only blocking findings, using distinct path counts
// for each category instead of a single conflated total.
func blockingHeader(c findingCounts) string {
	switch {
	case c.working == 0 && c.invalid == 0:
		return fmt.Sprintf("Missing committed merge base for %d managed file(s)", c.missing)
	case c.working == 0 && c.missing == 0:
		return fmt.Sprintf("Invalid committed merge base for %d managed file(s)", c.invalid)
	case c.working == 0:
		return fmt.Sprintf("Invalid committed merge base for %d managed file(s), and %d missing merge base(s)",
			c.invalid, c.missing)
	default:
		header := fmt.Sprintf("Drift detected in %d managed file(s)", c.working)
		if c.invalid > 0 {
			header += fmt.Sprintf("; %d invalid merge base(s)", c.invalid)
		}
		if c.missing > 0 {
			header += fmt.Sprintf("; %d missing merge base(s)", c.missing)
		}
		return header
	}
}

// printAdvisoryMissing prints advisory base-missing findings, capping the
// per-file detail so a large absent-base tree does not flood the output.
func printAdvisoryMissing(findings []drift.Finding) {
	if len(findings) <= advisoryBaseSampleLimit {
		printFindings(findings)
		return
	}
	printFindings(findings[:advisoryBaseSampleLimit])
	fmt.Printf("  ... and %d more missing base(s)\n", len(findings)-advisoryBaseSampleLimit)
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
			if f.Accepted {
				fmt.Printf("  %-12s %s (accepted %s)\n", f.Kind, f.Path, shortHash(f.Baseline))
			} else {
				fmt.Printf("  %-12s %s (recorded %s)\n", f.Kind, f.Path, shortHash(f.Recorded))
			}
		default:
			if f.Accepted {
				fmt.Printf("  %-12s %s (accepted %s, on disk %s)\n",
					f.Kind, f.Path, shortHash(f.Baseline), shortHash(f.Actual))
			} else {
				fmt.Printf("  %-12s %s (recorded %s, on disk %s)\n",
					f.Kind, f.Path, shortHash(f.Recorded), shortHash(f.Actual))
			}
		}
	}
}

// repairDrift reuses 'update' so repair and update share one reconciliation and
// transaction implementation. Local edits are preserved per --conflict-strategy.
func repairDrift(ctx context.Context, absPath, manifestPathArg string) error {
	resolvedTemplateDir, err := resolveTemplateDir(templateDirFlag)
	if err != nil {
		return err
	}

	cacheDir, registryNames := registryPolicy()
	rc := core.RunContext{
		TargetPath:          absPath,
		ManifestPathArg:     manifestPathArg,
		DryRun:              dryRun,
		Force:               force,
		ConflictStrategy:    driftStrategyFlag,
		TemplateDir:         resolvedTemplateDir,
		RegistryCacheDir:    cacheDir,
		RegistryNames:       registryNames,
		Ctx:                 ctx,
		AllowAncestorSearch: allowAncestorSearch,
	}
	// A dry run writes nothing, so a post-run Detect would still see every
	// pre-repair finding, including changes the plan would restore. Capture the
	// pre-repair findings instead and predict which of them survive.
	var preRepair []drift.Finding
	var preManifest *types.Manifest
	if dryRun {
		var loadErr error
		preManifest, preRepair, loadErr = loadDriftState(absPath)
		if loadErr != nil {
			return loadErr
		}
	}

	reporter := ui.NewReporter(runUpdateTotalSteps, dryRun)
	if err := core.RunUpdate(rc, reporter); err != nil {
		return err
	}
	if dryRun {
		reportPredictedResidual(preRepair, preManifest)
		return nil
	}
	return reportResidualDrift(absPath)
}

// loadDriftState resolves the manifest and its current drift findings without
// writing anything.
func loadDriftState(absPath string) (*types.Manifest, []drift.Finding, error) {
	m, rootPath, err := manifest.LoadWithOptions(absPath, manifest.DiscoveryOptions{
		AllowAncestorSearch: allowAncestorSearch,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load manifest: %w", err)
	}
	findings, err := drift.Detect(m, rootPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check drift: %w", err)
	}
	return m, findings, nil
}

// reportPredictedResidual prints the blocking findings a dry-run repair would
// leave behind. A dry run writes nothing, so predicting from the pre-repair scan
// is the only way to avoid listing files the real plan would restore (notably a
// deleted managed file).
func reportPredictedResidual(findings []drift.Finding, m *types.Manifest) {
	residual := predictedResidualFindings(findings, m)
	if len(residual) == 0 {
		return
	}

	fmt.Printf("Dry run: %d managed file(s) may remain drifted:\n", countFindingPaths(residual))
	printFindings(residual)
	if driftStrategyFlag != types.ConflictOverwrite {
		fmt.Println("Use --conflict-strategy overwrite to force restoration.")
	}
}

// predictedResidualFindings maps pre-repair findings to the blocking set that a
// dry-run repair may leave behind:
//
//   - Missing files are assumed restorable by the repair plan (the plan line
//     still shows their Create), unless they are listed as unmanaged.
//   - Modified files survive unless a canonical write repairs them (overwrite or
//     --force); unmanaged files are filtered from the plan, so they survive.
//   - BaseInvalid is repaired by a canonical write (overwrite or --force) and
//     survives the other strategies; an unmanaged path is not rewritten.
//   - BaseMissing is advisory for drift (and merge does not backfill it), so it
//     is never part of the blocking summary.
//
// This is a conservative heuristic: it may over-report a converged Modified
// file or a base a canonical write would repair, but it only suppresses findings
// the plan is expected to restore, so it never claims a false clean.
func predictedResidualFindings(findings []drift.Finding, m *types.Manifest) []drift.Finding {
	unmanaged := make(map[string]struct{})
	if m != nil {
		for _, key := range m.UnmanagedFiles {
			unmanaged[key] = struct{}{}
		}
	}
	restores := driftStrategyFlag == types.ConflictOverwrite || force

	residual := make([]drift.Finding, 0, len(findings))
	for _, f := range findings {
		_, detached := unmanaged[f.Path]
		if residualAfterRepair(f.Kind, restores, detached) {
			residual = append(residual, f)
		}
	}
	return residual
}

// residualAfterRepair reports whether a pre-repair finding survives the planned
// repair. restores marks a strategy that performs a canonical write (overwrite
// or --force); detached marks a path listed in unmanaged_files, which the plan
// filters out and therefore cannot restore.
func residualAfterRepair(kind drift.Kind, restores, detached bool) bool {
	switch kind {
	case drift.BaseMissing:
		// Advisory for drift, and merge does not backfill it.
		return false
	case drift.Missing:
		// Recreated by the repair plan unless the path is detached.
		return detached
	case drift.Modified, drift.BaseInvalid:
		// A canonical write rewrites the file and its base; other strategies
		// preserve the local state, and a detached path is never rewritten.
		return !restores || detached
	default:
		return true
	}
}

// reportResidualDrift re-checks the managed files after a real repair so a run
// that completed but could not clear everything (for example merge preserving
// local edits) cannot look like a clean repair. It shares partitionFindings and
// printFindings with the drift command so advisory classification, #223 path
// sanitization, and formatting stay in one place.
//
// Only blocking findings are reported: a missing committed base is advisory for
// `drift` and merge does not backfill it, so it must not be called residual
// drift. Residual findings do not change the exit contract: merge is documented
// to preserve local edits and still exits 0. Only a failure to perform the
// read-only re-check fails the command, so detection never reports a false
// clean.
func reportResidualDrift(absPath string) error {
	_, findings, err := loadDriftState(absPath)
	if err != nil {
		return fmt.Errorf("re-checking drift after repair: %w", err)
	}

	// A missing committed base is advisory for `drift` and merge does not
	// backfill it, so an advisory-only result is not residual drift. Report only
	// blocking findings; a clean working tree stays quiet.
	blocking, _ := partitionFindings(findings, false)
	if len(blocking) == 0 {
		return nil
	}

	fmt.Printf("Repair finished; %d managed file(s) still drifted:\n", countFindingPaths(blocking))
	printFindings(blocking)
	if driftStrategyFlag != types.ConflictOverwrite {
		fmt.Println("Use --conflict-strategy overwrite to force restoration.")
	}
	return nil
}

// countFindingPaths counts distinct managed files across findings, so a path
// with both a working-tree and a base finding is reported once.
func countFindingPaths(findings []drift.Finding) int {
	paths := make(map[string]struct{}, len(findings))
	for _, f := range findings {
		paths[f.Path] = struct{}{}
	}
	return len(paths)
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
