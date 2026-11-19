// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"scbake/internal/util/fileutil"
	"strings"

	"github.com/spf13/cobra"
)

// version is set at build time via the linker:
//
//	-ldflags "-X scbake/cmd.version={{ .Tag }}"
//
// See .goreleaser.yaml. It falls back to "dev" for local builds.
var version = "dev"

var (
	dryRun              bool
	force               bool
	templateDirFlag     string
	allowAncestorSearch bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "scbake",
	Short: "A manifest-driven project scaffolder",
	Long: `scbake is a single-binary CLI for scaffolding new projects
and applying layered infrastructure templates.`,

	// If the user just types 'scbake', show the version
	Run: func(cmd *cobra.Command, _ []string) {
		v, _ := cmd.Flags().GetBool("version")
		if v {
			fmt.Println(version)
			os.Exit(fileutil.ExitSuccess)
		}
		if err := cmd.Help(); err != nil {
			// This typically shouldn't fail, but check it for robustness.
			fmt.Fprintf(os.Stderr, "Error showing help: %v\n", err)
			os.Exit(fileutil.ExitError)
		}
	},
}

// helpOnNoArgs is the RunE for group commands that have no action of their
// own. Cobra treats a non-runnable parent as "show help" before it validates
// arguments, so a typo such as `scbake template pul` would otherwise print help
// and exit 0. Running instead lets the command reject unknown arguments.
func helpOnNoArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	if args[0] != "help" {
		return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
	}
	if len(args) == 1 {
		return cmd.Help()
	}
	return fmt.Errorf("unknown help topic %q", strings.Join(args[1:], " "))
}

// completeHelpTopics mirrors Cobra's default help-command completion: offer
// the commands available under the help topic resolved so far, including the
// help command itself, which IsAvailableCommand intentionally excludes.
func completeHelpTopics(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	target, _, err := cmd.Root().Find(args)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if target == nil {
		target = cmd.Root()
	}

	completions := make([]cobra.Completion, 0, len(target.Commands()))
	for _, subCmd := range target.Commands() {
		if !subCmd.IsAvailableCommand() && subCmd != helpCmd {
			continue
		}
		if strings.HasPrefix(subCmd.Name(), toComplete) {
			completions = append(completions, cobra.CompletionWithDesc(subCmd.Name(), subCmd.Short))
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// helpCmd replaces Cobra's default help command so an unknown topic is an
// error (non-zero exit) instead of a successful no-op.
var helpCmd = &cobra.Command{
	Use:   "help [command]",
	Short: "Help about any command",
	Long: `Help provides help for any command in the application.
Simply type scbake help [path to command] for full details.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := cmd.Root()
		if len(args) == 0 {
			return root.Help()
		}

		target, remaining, err := root.Find(args)
		if err != nil || len(remaining) > 0 || target == nil {
			return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
		}

		// Match the default help command: initialize the target's help and
		// version flags so they appear in the rendered help.
		target.InitDefaultHelpFlag()
		target.InitDefaultVersionFlag()
		return target.Help()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	// Go exits immediately on SIGINT/SIGTERM by default, which skips the
	// deferred rollback in RunApply. Divert those signals into the command
	// context so running tasks stop and the normal rollback path runs.
	ctx, stopSignals, receivedSignal := signalContext(context.Background())
	rootCmd.SetContext(ctx)

	err := rootCmd.Execute()
	stopSignals()

	if code := commandExitCode(err, receivedSignal()); code != fileutil.ExitSuccess {
		os.Exit(code)
	}
}

func init() {
	// Add persistent flags, available to all subcommands
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Show what changes would be made without executing them")
	rootCmd.PersistentFlags().BoolVar(&force, "force", false, "Override safety checks for file overwrites")
	rootCmd.PersistentFlags().StringVar(&templateDirFlag, "template-dir", os.Getenv("SCBAKE_TEMPLATE_DIR"), "Directory containing custom template overrides (default: $SCBAKE_TEMPLATE_DIR)")
	rootCmd.PersistentFlags().BoolVar(
		&allowAncestorSearch,
		"allow-ancestor-search",
		false,
		"Allow ancestor scbake.toml discovery when no Git root bounds the tree",
	)

	// Add a local version flag to the root command
	rootCmd.Flags().BoolP("version", "v", false, "Show the scbake version")

	// Add subcommands
	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(driftCmd)
	rootCmd.AddCommand(acceptCmd)
	rootCmd.AddCommand(unmanageCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(recoverCmd)

	// Initialize the generated completion command here so its RunE is installed
	// before execution; Execute() would otherwise add the default non-runnable
	// command, which treats an unknown shell as an argument and exits 0.
	rootCmd.InitDefaultCompletionCmd()
	for _, c := range rootCmd.Commands() {
		if c.Name() == "completion" {
			c.Args = cobra.ArbitraryArgs
			c.RunE = helpOnNoArgs
			break
		}
	}

	// Use a help command that reports unknown topics as errors. The completion
	// function is assigned here because it refers back to helpCmd itself.
	helpCmd.ValidArgsFunction = completeHelpTopics
	rootCmd.SetHelpCommand(helpCmd)
}
