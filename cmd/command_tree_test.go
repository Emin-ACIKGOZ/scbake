// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"scbake/internal/templateregistry"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestGroupCommands_UnknownSubcommandFails is a regression test for #215: a
// typo below a parent command that has no action of its own must be rejected
// with a non-zero exit, not treated as an argument that prints help and
// succeeds.
func TestGroupCommands_UnknownSubcommandFails(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)
	isolateXDGDirs(t)

	tests := []struct {
		name          string
		args          []string
		wantPath      string
		wantToken     string
		checkNoChange bool
	}{
		{
			name:      "template typo",
			args:      []string{"template", "bogus"},
			wantPath:  "scbake template",
			wantToken: "bogus",
		},
		{
			name:      "registry typo",
			args:      []string{"template", "registry", "bogus"},
			wantPath:  "scbake template registry",
			wantToken: "bogus",
		},
		{
			name:          "registry add typo",
			args:          []string{"template", "registry", "addd", "myreg", "https://example.com"},
			wantPath:      "scbake template registry",
			wantToken:     "addd",
			checkNoChange: true,
		},
		{
			name:      "pull typo",
			args:      []string{"template", "pul", "ci_github"},
			wantPath:  "scbake template",
			wantToken: "pul",
		},
		{
			name:      "completion shell typo",
			args:      []string{"completion", "bogus"},
			wantPath:  "scbake completion",
			wantToken: "bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executeCLI(tt.args...)
			if err == nil {
				t.Fatalf("%v exited 0, want an error", tt.args)
			}
			msg := err.Error()
			if !strings.Contains(msg, "unknown command") {
				t.Errorf("error should name the unknown command, got %q", msg)
			}
			if !strings.Contains(msg, tt.wantToken) {
				t.Errorf("error should contain %q, got %q", tt.wantToken, msg)
			}
			if !strings.Contains(msg, tt.wantPath) {
				t.Errorf("error should contain command path %q, got %q", tt.wantPath, msg)
			}

			if tt.checkNoChange {
				m, mErr := templateregistry.NewManager()
				if mErr != nil {
					t.Fatalf("NewManager failed: %v", mErr)
				}
				if got := m.List(); len(got) != 0 {
					t.Errorf("typo command added %d registry entr(ies), want 0", len(got))
				}
			}
		})
	}
}

// TestGroupCommands_NoArgsShowsHelp verifies the valid control paths: bare
// parent commands still print help and exit 0.
func TestGroupCommands_NoArgsShowsHelp(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	tests := []struct {
		name     string
		args     []string
		wantText string
	}{
		{name: "template", args: []string{"template"}, wantText: "scbake template"},
		{name: "registry", args: []string{"template", "registry"}, wantText: "scbake template registry"},
		{name: "completion", args: []string{"completion"}, wantText: "scbake completion"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			out := captureStdout(t, func() {
				err = executeCLI(tt.args...)
			})
			if err != nil {
				t.Fatalf("%v exited with error, want help: %v", tt.args, err)
			}
			if !strings.Contains(out, "Usage:") {
				t.Errorf("help output for %v has no usage section: %q", tt.args, out)
			}
			if !strings.Contains(out, tt.wantText) {
				t.Errorf("help output for %v should mention %q: %q", tt.args, tt.wantText, out)
			}
		})
	}
}

// TestGroupCommands_HelpArg verifies the conventional `<group> help` form
// still works without weakening typo rejection: help with a further argument
// is not a valid form at these levels.
func TestGroupCommands_HelpArg(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	tests := []struct {
		name     string
		args     []string
		wantText string
	}{
		{name: "template help", args: []string{"template", "help"}, wantText: "scbake template"},
		{name: "registry help", args: []string{"template", "registry", "help"}, wantText: "scbake template registry"},
		{name: "completion help", args: []string{"completion", "help"}, wantText: "scbake completion"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			out := captureStdout(t, func() {
				err = executeCLI(tt.args...)
			})
			if err != nil {
				t.Fatalf("%v exited with error, want help: %v", tt.args, err)
			}
			if !strings.Contains(out, "Usage:") {
				t.Errorf("help output for %v has no usage section: %q", tt.args, out)
			}
			if !strings.Contains(out, tt.wantText) {
				t.Errorf("help output for %v should mention %q: %q", tt.args, tt.wantText, out)
			}
		})
	}

	t.Run("help with topic fails", func(t *testing.T) {
		for _, args := range [][]string{
			{"template", "help", "bogus"},
			{"template", "registry", "help", "bogus"},
		} {
			err := executeCLI(args...)
			if err == nil {
				t.Fatalf("%v exited 0, want an error", args)
			}
			msg := err.Error()
			if !strings.Contains(msg, "unknown help topic") {
				t.Errorf("%v error should name an unknown help topic, got %q", args, msg)
			}
			if !strings.Contains(msg, "bogus") {
				t.Errorf("%v error should name the unknown topic, got %q", args, msg)
			}
			if strings.Contains(msg, `"help"`) {
				t.Errorf("%v error should not blame %q, got %q", args, "help", msg)
			}
		}
	})
}

// completionHas reports whether completions contains name, ignoring the
// description suffix that CompletionWithDesc adds after a tab.
func completionHas(completions []cobra.Completion, name string) bool {
	for _, c := range completions {
		if strings.SplitN(c, "\t", 2)[0] == name {
			return true
		}
	}
	return false
}

// assertCompletions checks the directive and that every wanted name is present
// while every rejected name is absent.
func assertCompletions(
	t *testing.T,
	completions []cobra.Completion,
	directive cobra.ShellCompDirective,
	want, reject []string,
) {
	t.Helper()

	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want NoFileComp", directive)
	}
	for _, name := range want {
		if !completionHas(completions, name) {
			t.Errorf("completions missing %q: %v", name, completions)
		}
	}
	for _, name := range reject {
		if completionHas(completions, name) {
			t.Errorf("completions should not include %q: %v", name, completions)
		}
	}
}

// TestHelpCommand_CompletesSubcommands ensures the custom help command kept
// Cobra's default argument completion. Without a ValidArgsFunction,
// `scbake __complete help ""` returned no completions instead of the commands
// available at the resolved level.
func TestHelpCommand_CompletesSubcommands(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	rootCmd.InitDefaultHelpCmd()
	fn := helpCmd.ValidArgsFunction
	if fn == nil {
		t.Fatal("help command has no ValidArgsFunction")
	}

	t.Run("root", func(t *testing.T) {
		completions, directive := fn(helpCmd, nil, "")
		assertCompletions(t, completions, directive, []string{"template", "help", "new"}, nil)
	})

	t.Run("prefix", func(t *testing.T) {
		completions, directive := fn(helpCmd, nil, "tem")
		assertCompletions(t, completions, directive, []string{"template"}, []string{"help"})
	})

	t.Run("nested topic", func(t *testing.T) {
		completions, directive := fn(helpCmd, []string{"template"}, "")
		assertCompletions(t, completions, directive, []string{"pull", "registry"}, []string{"help"})
	})

	t.Run("unknown topic", func(t *testing.T) {
		completions, directive := fn(helpCmd, []string{"bogus"}, "")
		if len(completions) != 0 {
			t.Errorf("unknown topic completions = %v, want none", completions)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("unknown topic directive = %v, want NoFileComp", directive)
		}
	})

	t.Run("hidden completion command", func(t *testing.T) {
		var err error
		out := captureStdout(t, func() {
			err = executeCLI("__complete", "help", "")
		})
		if err != nil {
			t.Fatalf("__complete failed: %v", err)
		}
		for _, want := range []string{"template\t", "help\t", ":4"} {
			if !strings.Contains(out, want) {
				t.Errorf("__complete help output missing %q: %q", want, out)
			}
		}
	})
}

// TestHelpCommand_Topics verifies the custom help command: known topics
// (including "help help") exit 0 with help, and unknown topics return an error.
func TestHelpCommand_Topics(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	tests := []struct {
		name     string
		args     []string
		wantText string
	}{
		{name: "root", args: []string{"help"}, wantText: "scbake"},
		{name: "command", args: []string{"help", "new"}, wantText: "scbake new"},
		{name: "nested command", args: []string{"help", "template", "registry"}, wantText: "scbake template registry"},
		{name: "help itself", args: []string{"help", "help"}, wantText: "scbake help"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			out := captureStdout(t, func() {
				err = executeCLI(tt.args...)
			})
			if err != nil {
				t.Fatalf("%v exited with error, want help: %v", tt.args, err)
			}
			if !strings.Contains(out, "Usage:") {
				t.Errorf("help output for %v has no usage section: %q", tt.args, out)
			}
			if !strings.Contains(out, tt.wantText) {
				t.Errorf("help output for %v should mention %q: %q", tt.args, tt.wantText, out)
			}
		})
	}

	unknown := [][]string{
		{"help", "bogus"},
		{"help", "template", "bogus"},
	}
	for _, args := range unknown {
		t.Run("unknown "+strings.Join(args[1:], " "), func(t *testing.T) {
			err := executeCLI(args...)
			if err == nil {
				t.Fatalf("%v exited 0, want an unknown help topic error", args)
			}
			if !strings.Contains(err.Error(), "unknown help topic") {
				t.Errorf("error should mention an unknown help topic, got %q", err.Error())
			}
			if !strings.Contains(err.Error(), args[len(args)-1]) {
				t.Errorf("error should name the unknown topic, got %q", err.Error())
			}
		})
	}
}

// TestCompletion_ValidShells verifies that installing the completion RunE did
// not change the generated shell completion commands.
func TestCompletion_ValidShells(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			if err := executeCLI("completion", shell); err != nil {
				t.Errorf("completion %s exited with error: %v", shell, err)
			}
		})
	}
}
