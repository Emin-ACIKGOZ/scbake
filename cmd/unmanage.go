// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"scbake/internal/core"

	"github.com/spf13/cobra"
)

var unmanageCmd = &cobra.Command{
	Use:   "unmanage <path>...",
	Short: "Detach managed files from scbake without deleting them",
	Long: `Detaches each managed file from scbake. The path is removed from
[managed_files] and [accepted_files], recorded in unmanaged_files, and its
committed base under .scbake-base is removed transactionally. The working file is
left exactly as it is.

After unmanage the path is no longer drift-gated and 'scbake update' or
'scbake drift --repair' will not rewrite, recreate, or re-record it. Running
'scbake apply' also skips unmanaged paths; only 'scbake apply --force' re-manages
one, writes canonical content, and clears the unmanaged entry. The root
.gitignore cannot be unmanaged because scbake maintains its internal-state
ignore block; use accept or edit that block manually.

Paths are resolved against the current directory and recorded root-relative, so
monorepo subprojects use keys like "services/api/main.go". A missing working
file is allowed because the state, not the bytes, is what is detached. Use
--dry-run to validate without writing.`,
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		rc, err := manageRunContext(cmd)
		if err != nil {
			return err
		}
		if err := core.RunUnmanage(rc, args); err != nil {
			return err
		}
		if rc.DryRun {
			fmt.Println("✅ Dry run complete; no changes written.")
		} else {
			fmt.Println("✅ Success! 'unmanage' command finished.")
		}
		return nil
	},
}
