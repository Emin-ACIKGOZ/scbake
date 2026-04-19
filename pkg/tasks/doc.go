// Package tasks defines the executable units of work used in a scaffolding plan.
//
// Task Types:
//   - CreateTemplateTask: Renders embedded templates and creates new files
//   - CreateDirectoryTask: Creates directories with transaction tracking
//   - ExecCommandTask: Executes shell commands with optional output tracking
//   - InsertXMLTask: Modifies existing XML files by inserting fragments (e.g., Maven pom.xml)
//
// All tasks implement the Task interface and support:
//   - Priority-based execution ordering (lower numbers first)
//   - Transaction safety (tracked for rollback on failure)
//   - Dry-run mode (DryRun flag suppresses side effects)
//   - Description strings for logging and reporting
//
// InsertXMLTask is specialized for XML modifications. It:
//   - Parses and validates existing XML files
//   - Inserts XML fragments at specified element paths (e.g., "/project/build/plugins")
//   - Maintains idempotency (no duplicates on repeated execution)
//   - Provides path validation to prevent directory traversal attacks
//   - Integrates with the transaction manager for safe rollback
package tasks
