// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"scbake/internal/types"
	"testing"
)

// issue138Reporter records the descriptions passed to TaskStart.
type issue138Reporter struct {
	descriptions []string
}

func (r *issue138Reporter) Step(string, string) {}
func (r *issue138Reporter) SetTotalSteps(int)   {}
func (r *issue138Reporter) TaskEnd(error)       {}
func (r *issue138Reporter) TaskStart(desc string, _, _ int) {
	r.descriptions = append(r.descriptions, desc)
}

// issue138SourceTask counts description/execution calls and implements
// types.SourceDescriber.
type issue138SourceTask struct {
	desc        string
	sourceCalls int
	execCalls   int

	// observedAllowlist records the task context the executor supplied.
	observedAllowlist []string
}

func (t *issue138SourceTask) Description() string { return t.desc }
func (t *issue138SourceTask) Priority() int       { return 10 }

func (t *issue138SourceTask) Execute(tc types.TaskContext) error {
	t.execCalls++
	t.observedAllowlist = tc.RegistryNames
	return nil
}

func (t *issue138SourceTask) DescriptionWithSource(tc types.TaskContext) string {
	t.sourceCalls++
	return t.desc + " [registry: " + tc.RegistryNames[0] + "]"
}

// TestIssue138_ExecutorSourceDisclosureIsDryRunOnly pins that the executor
// consults SourceDescriber for previews only; normal runs keep the plain
// description and still execute the task.
func TestIssue138_ExecutorSourceDisclosureIsDryRunOnly(t *testing.T) {
	t.Run("dry run discloses source", func(t *testing.T) {
		task := &issue138SourceTask{desc: "Create file"}
		reporter := &issue138Reporter{}
		tc := types.TaskContext{DryRun: true, RegistryNames: []string{"team"}}

		if err := Execute(&types.Plan{Tasks: []types.Task{task}}, tc, reporter); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if task.sourceCalls != 1 {
			t.Errorf("DescriptionWithSource calls = %d, want 1", task.sourceCalls)
		}
		if task.execCalls != 0 {
			t.Errorf("Execute calls = %d, want 0 in dry-run", task.execCalls)
		}
		if len(reporter.descriptions) != 1 || reporter.descriptions[0] != "Create file [registry: team]" {
			t.Errorf("reported descriptions = %v, want the source-annotated description", reporter.descriptions)
		}
	})

	t.Run("real run keeps plain description", func(t *testing.T) {
		task := &issue138SourceTask{desc: "Create file"}
		reporter := &issue138Reporter{}
		tc := types.TaskContext{DryRun: false, RegistryNames: []string{"team"}}

		if err := Execute(&types.Plan{Tasks: []types.Task{task}}, tc, reporter); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if task.sourceCalls != 0 {
			t.Errorf("DescriptionWithSource called %d times on a real run, want 0", task.sourceCalls)
		}
		if task.execCalls != 1 {
			t.Errorf("Execute calls = %d, want 1", task.execCalls)
		}
		if len(reporter.descriptions) != 1 || reporter.descriptions[0] != "Create file" {
			t.Errorf("reported descriptions = %v, want the plain description", reporter.descriptions)
		}
		if len(task.observedAllowlist) != 1 || task.observedAllowlist[0] != "team" {
			t.Errorf("task context RegistryNames = %v, want [team]", task.observedAllowlist)
		}
	})
}
