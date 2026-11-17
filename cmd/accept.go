// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"path/filepath"
	"scbake/internal/core"

	"github.com/spf13/cobra"
)

var acceptCmd = &cobra.Command{
	Use:   "accept <path>...",
	Short: "Accept current bytes as the drift baseline for managed files",
	Long: `Records the current working-tree bytes of each managed file as its accepted
drift baseline. The file is not rewritten, and the pristine managed_files hash
and the committed base under .scbake-base are left untouched: 'scbake drift'
compares the working tree against the accepted hash, while base validation still
uses the pristine hash. Use 'scbake unmanage' to stop tracking a file instead.

Accept is a one-shot rebaseline. A later edit drifts again, and a template change
that writes canonical content clears the acceptance, so review and re-accept
after a template update. Paths are resolved against the current directory and
recorded root-relative, so monorepo subprojects use keys like
"services/api/main.go". Use --dry-run to validate without writing.`,
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		rc, err := manageRunContext(cmd)
		if err != nil {
			return err
		}
		if err := core.RunAccept(rc, args); err != nil {
			return err
		}
		if rc.DryRun {
			fmt.Println("✅ Dry run complete; no changes written.")
		} else {
			fmt.Println("✅ Success! 'accept' command finished.")
		}
		return nil
	},
}

// manageRunContext builds the run context for a command whose file arguments are
// resolved against the invocation directory. accept/unmanage never render
// templates, so template and registry settings are not needed.
func manageRunContext(cmd *cobra.Command) (core.RunContext, error) {
	absPath, err := filepath.Abs(".")
	if err != nil {
		return core.RunContext{}, fmt.Errorf("resolving path: %w", err)
	}
	return core.RunContext{
		TargetPath:          absPath,
		ManifestPathArg:     ".",
		DryRun:              dryRun,
		Force:               force,
		Ctx:                 cmd.Context(),
		AllowAncestorSearch: allowAncestorSearch,
	}, nil
}
