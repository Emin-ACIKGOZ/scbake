// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"fmt"
	"scbake/internal/types"
	"sort"
)

// Execute runs the plan and reports progress via the provided Reporter.
func Execute(plan *types.Plan, tc types.TaskContext, reporter types.Reporter) error {
	sort.SliceStable(plan.Tasks, func(i, j int) bool {
		return plan.Tasks[i].Priority() < plan.Tasks[j].Priority()
	})

	for i, task := range plan.Tasks {
		// File tasks do not block on the context, so check between tasks as
		// well. A signal that arrives during one task still stops the plan
		// before the next task and lets the deferred rollback run.
		if err := contextError(tc.Ctx); err != nil {
			return fmt.Errorf("execution interrupted before %q: %w", task.Description(), err)
		}

		reporter.TaskStart(task.Description(), i+1, len(plan.Tasks))

		var err error
		if !tc.DryRun {
			err = task.Execute(tc)
		}

		reporter.TaskEnd(err)

		if err != nil {
			return fmt.Errorf("task failed (%s): %w", task.Description(), err)
		}
	}

	return nil
}

// contextError reports a canceled context, tolerating the zero TaskContext used
// by tests and library callers that do not configure one.
func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
