// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// plannedProducer is a task that promises to create paths and performs no
// side effects in either mode. It lets the test isolate how the executor
// carries CreatedPathProvider declarations between tasks.
type plannedProducer struct {
	paths []string
}

func (plannedProducer) Description() string { return "produce planned artifact" }
func (plannedProducer) Priority() int       { return 1 }
func (plannedProducer) Validate(types.TaskContext) error {
	return nil
}
func (plannedProducer) Execute(types.TaskContext) error { return nil }
func (p plannedProducer) CreatedPaths(types.TaskContext) []string {
	return p.paths
}

func plannedPathTaskContext(target string, dryRun bool) types.TaskContext {
	return types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: target,
		Manifest:   &types.Manifest{SbakeVersion: "v1.0.0"},
		DryRun:     dryRun,
	}
}

func plannedInsertTask() *tasks.InsertXMLTask {
	return &tasks.InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin><artifactId>maven-checkstyle-plugin</artifactId></plugin>`,
		Desc:        "insert plugin",
		TaskPrio:    2,
	}
}

// TestDryRunPlannedPathsDeferMissingTarget is a regression for #144: a task may
// target an artifact an earlier task in the same plan creates. The dry run must
// defer the missing target, while a real run with the same plan must still fail
// because the producer never actually created it.
func TestDryRunPlannedPathsDeferMissingTarget(t *testing.T) {
	target := t.TempDir()
	pom := filepath.Join(target, "pom.xml")
	plan := func() *types.Plan {
		return &types.Plan{Tasks: []types.Task{
			plannedProducer{paths: []string{pom}},
			plannedInsertTask(),
		}}
	}

	if err := Execute(plan(), plannedPathTaskContext(target, true), dryRunNoopReporter{}); err != nil {
		t.Fatalf("dry run = %v, want nil when an earlier task plans pom.xml", err)
	}
	if _, err := os.Stat(pom); !os.IsNotExist(err) {
		t.Fatalf("dry run created pom.xml (stat err: %v)", err)
	}

	err := Execute(plan(), plannedPathTaskContext(target, false), dryRunNoopReporter{})
	if err == nil {
		t.Fatal("real run = nil, want the missing target error")
	}
	want := "task failed (insert plugin): XML file not found: pom.xml"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("real run error = %q, want it to contain %q", err, want)
	}
	if _, statErr := os.Stat(pom); !os.IsNotExist(statErr) {
		t.Fatalf("real run created pom.xml (stat err: %v)", statErr)
	}
}

// TestDryRunWithoutPlannedPathsFails pins that the deferral only applies to
// declared paths: a missing target with no producer still fails the dry run.
func TestDryRunWithoutPlannedPathsFails(t *testing.T) {
	target := t.TempDir()
	plan := &types.Plan{Tasks: []types.Task{plannedInsertTask()}}

	err := Execute(plan, plannedPathTaskContext(target, true), dryRunNoopReporter{})
	if err == nil {
		t.Fatal("dry run = nil, want XML file not found")
	}
	want := "XML file not found: pom.xml"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("dry run error = %q, want it to contain %q", err, want)
	}
}

// TestDryRunPlannedUnrelatedPathDoesNotDefer pins that a declaration for a
// different path does not suppress a genuine missing-target failure.
func TestDryRunPlannedUnrelatedPathDoesNotDefer(t *testing.T) {
	target := t.TempDir()
	plan := &types.Plan{Tasks: []types.Task{
		plannedProducer{paths: []string{filepath.Join(target, "other.xml")}},
		plannedInsertTask(),
	}}

	err := Execute(plan, plannedPathTaskContext(target, true), dryRunNoopReporter{})
	if err == nil {
		t.Fatal("dry run = nil, want XML file not found for the undeclared target")
	}
	if !strings.Contains(err.Error(), "XML file not found: pom.xml") {
		t.Errorf("dry run error = %q, want the missing-target error", err)
	}
}

// TestDryRunPlannedTargetIsStillStructurallyValidated pins that deferral does
// not skip the checks that need no existing file: a malformed element path or
// snippet must still fail.
func TestDryRunPlannedTargetIsStillStructurallyValidated(t *testing.T) {
	target := t.TempDir()
	pom := filepath.Join(target, "pom.xml")

	badPath := plannedInsertTask()
	badPath.ElementPath = "///"
	badSnippet := plannedInsertTask()
	badSnippet.XMLContent = "<unclosed>"

	base := plannedPathTaskContext(target, true)
	base.PlannedPaths = []string{pom}
	for name, task := range map[string]types.Task{"element path": badPath, "snippet": badSnippet} {
		t.Run(name, func(t *testing.T) {
			err := Execute(&types.Plan{Tasks: []types.Task{task}}, base, dryRunNoopReporter{})
			if err == nil {
				t.Fatal("dry run = nil, want the structural validation error")
			}
		})
	}
}
