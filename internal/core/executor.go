package core

import (
	"fmt"
	"scbake/internal/types"
	"sort"
)

// Execute runs the plan.
// This is the core engine of scbake.
func Execute(plan *types.Plan, tc types.TaskContext) error {
	// Sort tasks by priority (lower numbers run first)
	sort.SliceStable(plan.Tasks, func(i, j int) bool {
		return plan.Tasks[i].Priority() < plan.Tasks[j].Priority()
	})

	// Run tasks sequentially
	for i, task := range plan.Tasks {
		// This is the simple logging we discussed
		fmt.Printf("[%d/%d] 🚀 Executing: %s\n", i+1, len(plan.Tasks), task.Description())

		if err := task.Execute(tc); err != nil {
			// If any task fails, we stop and return the error.
			// The caller (e.g., 'apply' command) will handle the rollback.
			return fmt.Errorf("task failed (%s): %w", task.Description(), err)
		}
	}

	return nil
}
