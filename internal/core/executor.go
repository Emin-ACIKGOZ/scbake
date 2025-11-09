package core

import (
	"fmt"
	"scbake/internal/types"
	"sort"
	"time"
)

// Braille spinner characters
var spinnerChars = []string{"⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽", "⣾"}

// Execute runs the plan.
func Execute(plan *types.Plan, tc types.TaskContext) error {

	// Sort tasks by priority
	sort.SliceStable(plan.Tasks, func(i, j int) bool {
		return plan.Tasks[i].Priority() < plan.Tasks[j].Priority()
	})

	for i, task := range plan.Tasks {

		// If we're in a dry run, just print the description
		if tc.DryRun {
			fmt.Printf("  [DRY RUN] %s\n", task.Description())
			continue
		}

		// Spinner Logic
		done := make(chan struct{})
		go func() {
			j := 0
			for {
				select {
				case <-done:
					return
				default:
					// Print the spinner, prefix, and description
					prefix := fmt.Sprintf("[%d/%d]", i+1, len(plan.Tasks))
					line := fmt.Sprintf("\r%s %s %s", prefix, spinnerChars[j%len(spinnerChars)], task.Description())
					fmt.Print(line)
					j++
					time.Sleep(100 * time.Millisecond)
				}
			}
		}()

		err := task.Execute(tc) // Run the actual task

		close(done) // Stop the spinner goroutine

		// Print the final line
		prefix := fmt.Sprintf("[%d/%d]", i+1, len(plan.Tasks))
		if err != nil {
			// Print a clear error message, overwriting the spinner line
			fmt.Printf("\r%s ❌ %s\n", prefix, task.Description())
			return fmt.Errorf("task failed (%s): %w", task.Description(), err)
		}

		// Print a clear success message, overwriting the spinner line
		fmt.Printf("\r%s ✅ %s\n", prefix, task.Description())
	}

	return nil
}
