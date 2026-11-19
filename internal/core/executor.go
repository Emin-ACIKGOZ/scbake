// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"fmt"
	"path/filepath"
	"scbake/internal/types"
	"slices"
	"sort"
	"strings"
)

// Execute runs the plan and reports progress via the provided Reporter.
func Execute(plan *types.Plan, tc types.TaskContext, reporter types.Reporter) error {
	return executePlan(plan, tc, reporter, 0, nil)
}

// executePlan runs tasks in priority order. When between is non-nil it runs
// once immediately before the first task whose priority is at least
// splitPriority, or after the last task when no task reaches that priority.
// Callers use this to persist state that later-priority tasks must observe,
// such as the finalized manifest before version-control tasks commit it.
func executePlan(
	plan *types.Plan,
	tc types.TaskContext,
	reporter types.Reporter,
	splitPriority int,
	between func() error,
) error {
	sort.SliceStable(plan.Tasks, func(i, j int) bool {
		return plan.Tasks[i].Priority() < plan.Tasks[j].Priority()
	})

	betweenRun := between == nil
	for i, task := range plan.Tasks {
		if !betweenRun && task.Priority() >= splitPriority {
			if err := between(); err != nil {
				return err
			}
			betweenRun = true
		}

		if err := runTask(plan, i, task, tc, reporter); err != nil {
			return err
		}

		// A later task's Validate may need to know that an artifact does not
		// exist yet only because an earlier task is expected to create it.
		if tc.DryRun {
			tc = carryPlannedPaths(tc, task)
		}
	}

	if !betweenRun {
		return between()
	}
	return nil
}

// runTask checks for cancellation, emits start/end progress, and executes a
// single task. It returns a task-scoped error on failure.
func runTask(plan *types.Plan, index int, task types.Task, tc types.TaskContext, reporter types.Reporter) error {
	// File tasks do not block on the context, so check between tasks as
	// well. A signal that arrives during one task still stops the plan
	// before the next task and lets the deferred rollback run.
	if err := contextError(tc.Ctx); err != nil {
		return fmt.Errorf("execution interrupted before %q: %w", task.Description(), err)
	}

	desc := task.Description()
	var err error
	if tc.DryRun {
		if sd, ok := task.(types.SourceDescriber); ok {
			desc = sd.DescriptionWithSource(tc)
		}
		// A dry run reports what the real run would decide, without
		// performing any of it. Validate is the read-only half of a task,
		// so this path can never write. Running it before TaskStart lets a
		// task annotate its description with the decision it just made; the
		// note is recorded by Validate because that is the only place that
		// holds the shared reconciliation result (see types.DryRunAnnotator).
		err = task.Validate(tc)
		if verb := dryRunVerb(task); verb != "" {
			desc = replaceLeadingWord(desc, verb)
		}
		if note := dryRunNote(task); note != "" {
			desc += " (" + note + ")"
		}
	}
	reporter.TaskStart(desc, index+1, len(plan.Tasks))

	if !tc.DryRun {
		// A real run executes; the dry run already ran Validate above.
		err = task.Execute(tc)
	}

	reporter.TaskEnd(err)

	if err != nil {
		return fmt.Errorf("task failed (%s): %w", task.Description(), err)
	}
	return nil
}

// dryRunNote returns a task's optional dry-run annotation. Tasks that do not
// implement types.DryRunAnnotator contribute no note.
func dryRunNote(task types.Task) string {
	annotator, ok := task.(types.DryRunAnnotator)
	if !ok {
		return ""
	}
	return annotator.DryRunNote()
}

// dryRunVerb returns a task's optional leading-verb override. Tasks that do not
// implement types.DryRunVerbProvider keep their plain description.
func dryRunVerb(task types.Task) string {
	provider, ok := task.(types.DryRunVerbProvider)
	if !ok {
		return ""
	}
	return provider.DryRunVerb()
}

// replaceLeadingWord substitutes verb for desc's first space-delimited word,
// preserving the remainder (and any source suffix). A one-word description
// becomes just the verb.
func replaceLeadingWord(desc, verb string) string {
	if i := strings.IndexByte(desc, ' '); i >= 0 {
		return verb + desc[i:]
	}
	return verb
}

// carryPlannedPaths records the paths task declared it will create, so a later
// task's read-only validation can treat them as "not yet created". Declared
// paths are absolute (see types.CreatedPathProvider).
func carryPlannedPaths(tc types.TaskContext, task types.Task) types.TaskContext {
	provider, ok := task.(types.CreatedPathProvider)
	if !ok {
		return tc
	}
	declared := provider.CreatedPaths(tc)
	if len(declared) == 0 {
		return tc
	}
	paths := slices.Clone(tc.PlannedPaths)
	for _, path := range declared {
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if slices.Contains(paths, path) {
			continue
		}
		paths = append(paths, path)
	}
	tc.PlannedPaths = paths
	return tc
}

// contextError reports a canceled context, tolerating the zero TaskContext used
// by tests and library callers that do not configure one.
func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
