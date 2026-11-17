// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/types"
)

// plannedInsertContext builds a TaskContext whose plan already declared the
// insertion target, so a dry run may defer a missing file.
func plannedInsertContext(target string, planned bool, dryRun bool) types.TaskContext {
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: target,
		Manifest:   &types.Manifest{SbakeVersion: "v1.0.0"},
		DryRun:     dryRun,
	}
	if planned {
		tc.PlannedPaths = []string{filepath.Join(target, "pom.xml")}
	}
	return tc
}

func plannedInsertTask() *InsertXMLTask {
	return &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  `<plugin><artifactId>maven-checkstyle-plugin</artifactId></plugin>`,
		Desc:        "insert plugin",
		TaskPrio:    1201,
	}
}

// TestInsertXMLTask_ValidateDeferralIsDryRunAndPlanOnly is a regression for
// #144: only a dry run with a declared predecessor may treat a missing target
// as "not yet created". Every other combination keeps the historic error and
// writes nothing.
func TestInsertXMLTask_ValidateDeferralIsDryRunAndPlanOnly(t *testing.T) {
	target := t.TempDir()
	pom := filepath.Join(target, "pom.xml")
	task := plannedInsertTask()

	if err := task.Validate(plannedInsertContext(target, true, true)); err != nil {
		t.Fatalf("Validate(dry, planned) = %v, want nil", err)
	}
	if err := task.Execute(plannedInsertContext(target, true, true)); err != nil {
		t.Fatalf("Execute(dry, planned) = %v, want nil", err)
	}
	if _, err := os.Stat(pom); !os.IsNotExist(err) {
		t.Fatalf("dry run created pom.xml (stat err: %v)", err)
	}

	for name, run := range map[string]func() error{
		"validate dry unplanned": func() error {
			return task.Validate(plannedInsertContext(target, false, true))
		},
		"validate real planned": func() error {
			return task.Validate(plannedInsertContext(target, true, false))
		},
		"execute real planned": func() error {
			return task.Execute(plannedInsertContext(target, true, false))
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := run()
			if err == nil {
				t.Fatal("got nil, want XML file not found")
			}
			if !strings.Contains(err.Error(), "XML file not found: pom.xml") {
				t.Errorf("error = %q, want the historic missing-file text", err)
			}
			if _, statErr := os.Stat(pom); !os.IsNotExist(statErr) {
				t.Fatalf("failed path created pom.xml (stat err: %v)", statErr)
			}
		})
	}
}

// TestInsertXMLTask_InsertionErrorTextUnchanged pins that the double-parse
// refactor returns the same splice error from Validate and Execute and does not
// write the target on failure.
func TestInsertXMLTask_InsertionErrorTextUnchanged(t *testing.T) {
	h := newTestHelper(t)
	before, err := h.readPom()
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	task := &InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/nonexistent",
		XMLContent:  `<plugin/>`,
		Desc:        "missing element",
		TaskPrio:    1201,
	}
	want := "failed to insert XML into pom.xml: target element path /project/build/nonexistent not found"

	validateErr := task.Validate(h.taskContext(true, false))
	executeErr := task.Execute(h.taskContext(false, false))
	for name, err := range map[string]error{"Validate": validateErr, "Execute": executeErr} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s error = %v, want it to contain %q", name, err, want)
		}
	}
	if validateErr != nil && executeErr != nil && validateErr.Error() != executeErr.Error() {
		t.Errorf("validation and execution disagree:\nvalidate: %v\nexecute:  %v", validateErr, executeErr)
	}

	after, err := h.readPom()
	if err != nil {
		t.Fatalf("re-reading fixture: %v", err)
	}
	if after != before {
		t.Error("failed insertion modified pom.xml")
	}
}
