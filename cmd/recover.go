// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"

	"github.com/spf13/cobra"
)

var recoverDiscardFlag bool

var recoverCmd = &cobra.Command{
	Use:   "recover [path]",
	Short: "Roll back an interrupted transaction",
	Long: `Finds a transaction left behind by a crash or SIGKILL and rolls it back from
its on-disk journal, restoring backed-up files and deleting paths the interrupted
run created.

Recovery refuses to touch a transaction whose process is still running. A
journal without a matching local run marker may have arrived through a
checkout or a copy; pass --force to replay it anyway, or --discard to delete
the transaction directory without restoring its backups. Recovering an
interrupted 'new' removes the bootstrap project directory as a whole.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		targetPath := "."
		if len(args) > 0 {
			targetPath = args[0]
		}

		absPath, err := filepath.Abs(targetPath)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		rootPath, err := manifest.FindProjectRootWithOptions(absPath, manifest.DiscoveryOptions{
			AllowAncestorSearch: allowAncestorSearch,
		})
		if err != nil {
			return fmt.Errorf("finding project root: %w", err)
		}

		if dryRun {
			pending, err := transaction.ListPending(rootPath)
			if err != nil {
				return err
			}
			for _, tx := range pending {
				if tx.Alive {
					return fmt.Errorf("transaction in %s is still running (pid %d)", tx.Dir, tx.PID)
				}
				action := "recover"
				if recoverDiscardFlag {
					action = "discard"
				} else if !tx.Trusted && !force {
					return fmt.Errorf(
						"transaction in %s has no local run marker; rerun with --force to replay it or --discard to remove it",
						tx.Dir,
					)
				}
				fmt.Printf("Would %s transaction in %s (pid %d).\n", action, tx.Dir, tx.PID)
			}
			if len(pending) == 0 {
				fmt.Println("No interrupted transaction found.")
			}
			return nil
		}

		recovered, err := transaction.RecoverWithOptions(rootPath, transaction.RecoverOptions{
			Discard: recoverDiscardFlag,
			Force:   force,
		})
		if err != nil {
			return err
		}
		if recovered == 0 {
			fmt.Println("No interrupted transaction found.")
			return nil
		}
		if recoverDiscardFlag {
			fmt.Printf("Discarded %d interrupted transaction(s).\n", recovered)
			return nil
		}
		fmt.Printf("Recovered %d interrupted transaction(s).\n", recovered)
		return nil
	},
}

func init() {
	recoverCmd.Flags().BoolVar(
		&recoverDiscardFlag,
		"discard",
		false,
		"Delete an unrecoverable transaction without restoring its backups",
	)
}
