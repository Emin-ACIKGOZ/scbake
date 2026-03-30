// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package core

import (
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
