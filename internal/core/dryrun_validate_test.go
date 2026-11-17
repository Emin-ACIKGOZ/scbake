// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"scbake/internal/types"
	"strings"
	"testing"
)

// dryRunValidateFailureTask has a failing read-only Validate and a successful
// Execute. A dry run must report the Validate failure; a real run must not.
type dryRunValidateFailureTask struct{}

func (dryRunValidateFailureTask) Description() string { return "failing validation" }
func (dryRunValidateFailureTask) Priority() int       { return 1 }

func (dryRunValidateFailureTask) Validate(types.TaskContext) error {
	return errors.New("read-only validation failed")
}

func (dryRunValidateFailureTask) Execute(types.TaskContext) error { return nil }

type dryRunNoopReporter struct{}

func (dryRunNoopReporter) Step(string, string)        {}
func (dryRunNoopReporter) SetTotalSteps(int)          {}
func (dryRunNoopReporter) TaskStart(string, int, int) {}
func (dryRunNoopReporter) TaskEnd(error)              {}

// TestDryRunCallsValidateAndPropagatesFailure is a regression for #144: the
// dry-run executor skipped task bodies entirely, so a read-only validation
// failure was reported as success.
func TestDryRunCallsValidateAndPropagatesFailure(t *testing.T) {
	err := Execute(
		&types.Plan{Tasks: []types.Task{dryRunValidateFailureTask{}}},
		types.TaskContext{DryRun: true},
		dryRunNoopReporter{},
	)
	if err == nil {
		t.Fatal("Execute(dry-run) = nil, want the task's validation error")
	}
	want := "task failed (failing validation): read-only validation failed"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Execute(dry-run) error = %q, want it to contain %q", err, want)
	}
}

// TestRealRunCallsExecuteNotValidate pins the other half of the contract: a
// real run invokes Execute and is not blocked by a read-only validation failure.
func TestRealRunCallsExecuteNotValidate(t *testing.T) {
	err := Execute(
		&types.Plan{Tasks: []types.Task{dryRunValidateFailureTask{}}},
		types.TaskContext{DryRun: false},
		dryRunNoopReporter{},
	)
	if err != nil {
		t.Fatalf("Execute(real) = %v, want nil", err)
	}
}
