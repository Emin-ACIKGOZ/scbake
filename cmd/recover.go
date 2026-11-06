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
the transaction directory without restoring its backups. Inspect an untrusted
journal before using --force: replaying one that marks remove_root deletes the
entire project directory. Recovering an interrupted 'new' removes the
bootstrap project directory as a whole.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		targetPath := "."
		if len(args) > 0 {
			targetPath = args[0]
		}
		if err := ctx.Err(); err != nil {
			return err
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
		// Resolve the root once so the dry-run disclosure and RemoveRoot both
		// name the real project directory, not an alias symlink.
		rootPath, err = filepath.EvalSymlinks(rootPath)
		if err != nil {
			return fmt.Errorf("resolving project root: %w", err)
		}

		if dryRun {
			pending, err := transaction.ListPendingContext(ctx, rootPath)
			if err != nil {
				return err
			}
			for _, tx := range pending {
				if err := ctx.Err(); err != nil {
					return err
				}
				if tx.Alive {
					return fmt.Errorf("transaction in %s is still running (pid %d)", tx.Dir, tx.PID)
				}
				if tx.LivenessUnknown && !recoverDiscardFlag && !force {
					detail := ""
					if tx.RemoveRoot {
						detail = " and replaying it would remove the entire project root"
					}
					return fmt.Errorf(
						"transaction in %s may still be running (pid %d); its liveness cannot be verified%s: rerun with --force to replay it or --discard to remove it",
						tx.Dir, tx.PID, detail,
					)
				}
				action := "recover"
				if recoverDiscardFlag {
					action = "discard"
				} else if !tx.Trusted && !force {
					detail := ""
					if tx.RemoveRoot {
						detail = "; replaying it would remove the entire project root"
					}
					return fmt.Errorf(
						"transaction in %s has no local run marker%s; rerun with --force to replay it or --discard to remove it",
						tx.Dir, detail,
					)
				}
				if action == "recover" && tx.RemoveRoot {
					fmt.Printf(
						"Would recover transaction in %s (pid %d) and remove the entire project root %s.\n",
						tx.Dir, tx.PID, rootPath,
					)
					continue
				}
				fmt.Printf("Would %s transaction in %s (pid %d).\n", action, tx.Dir, tx.PID)
			}
			if len(pending) == 0 {
				fmt.Println("No interrupted transaction found.")
			}
			return ctx.Err()
		}

		recovered, err := transaction.RecoverWithOptions(rootPath, transaction.RecoverOptions{
			Ctx:     ctx,
			Discard: recoverDiscardFlag,
			Force:   force,
		})
		if err != nil {
			return err
		}
		// Do not report success or exit 0 if a signal arrived while recovery
		// was running; the caller maps the context error to 128+signal.
		if err := ctx.Err(); err != nil {
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
