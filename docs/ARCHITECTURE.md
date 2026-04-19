# scbake Architecture

This document explains scbake's design for contributors and maintainers.

## Overview

scbake is a **manifest-driven, atomic project scaffolder** built on three core concepts:

1. **Handlers** - Plugins (language packs, templates) that define what to create
2. **Tasks** - Atomic operations (create files, run commands) that handlers compose
3. **Transactions** - LIFO (Last-In-First-Out) rollback system for atomicity

```
User Command (scbake new)
    ↓
Handler Discovery & Registration
    ↓
Handler.GetTasks() → []Task
    ↓
Priority Sorting (execute in order)
    ↓
Task Execution (with transaction tracking)
    ↓
On success: Commit (delete backups)
On failure: Rollback (restore backups, LIFO order)
```

---

## Core Components

### 1. Handler Interface

All language packs and templates implement this interface:

```go
// internal/types/plan.go
type Handler interface {
    GetTasks(targetPath string) ([]types.Task, error)
}
```

**Responsibility**: Given a project path, return a list of tasks to execute.

**Examples**:
- `pkg/lang/go/go.go` - Go language handler
- `pkg/templates/makefile/makefile.go` - Makefile template

**Key insight**: Handlers are **pure functions** that produce tasks. They don't execute anything—they just plan.

### 2. Task Interface

All operations (file creation, command execution, etc.) implement this interface:

```go
// internal/types/task.go (simplified)
type Task interface {
    Description() string
    Priority() int
    Execute(tc TaskContext) error
}
```

**Four built-in task types**:

| Type | Purpose | Example |
|------|---------|---------|
| **CreateTemplateTask** | Create files from Go templates | Create `Makefile` from template |
| **ExecCommandTask** | Run shell commands | `go mod init` |
| **CreateDirectoryTask** | Create directory structure | Create `src/` directory |
| **InsertXMLTask** | Insert XML into existing files | Add plugin to `pom.xml` |

**Key insight**: Tasks are **self-contained operations** with built-in error handling and rollback support.

### 3. Transaction System

scbake's transaction system provides **LIFO rollback** for atomicity:

```
Execute Task 1 → Backup created files
Execute Task 2 → Backup created files
Execute Task 3 → FAILS
    ↓
Rollback Task 3 (remove files)
Rollback Task 2 (restore from backup)
Rollback Task 1 (restore from backup)
    ↓
Project restored to original state
```

**Location**: `internal/filesystem/transaction/`

**Key features**:
- ✅ Tracks created files and directory changes
- ✅ Backs up modified files before overwrite
- ✅ LIFO rollback order (last-in, first-out)
- ✅ Atomic rename for manifest updates
- ✅ Cleanup of orphaned backups

---

## Execution Flow

### Loading & Discovery

```go
// cmd/apply.go → internal/core/run.go
func RunApply(rc RunContext, reporter types.Reporter) error {
    // 1. Discover project root & load manifest
    m, rootPath, err := manifest.Load(rc.TargetPath)
    
    // 2. Initialize transaction system
    tx, err := transaction.New(rootPath)
    
    // 3. Build execution plan
    plan, _, changes, err := buildPlan(rc)
}
```

**What happens**:
1. Find `scbake.toml` (or create empty manifest if new project)
2. Create backup system (`.scbake/tmp/` directory)
3. Build list of tasks to execute

### Phase 2: Planning

```go
// internal/core/run.go
func buildPlan(rc RunContext) (*types.Plan, string, *manifestChanges, error) {
    // 1. Get language handler (if --lang specified)
    handler, err := lang.GetHandler(rc.LangFlag)
    langTasks, err := handler.GetTasks(rc.TargetPath)
    plan.Tasks = append(plan.Tasks, langTasks...)
    
    // 2. Get template handlers (if --with specified)
    for _, tmpl := range rc.WithFlag {
        handler, err := templates.GetHandler(tmpl)
        tmplTasks, err := handler.GetTasks(rc.TargetPath)
        plan.Tasks = append(plan.Tasks, tmplTasks...)
    }
    
    // 3. Sort by priority
    sort.Slice(plan.Tasks, func(i, j int) bool {
        return plan.Tasks[i].Priority() < plan.Tasks[j].Priority()
    })
}
```

**What happens**:
1. Call each handler's `GetTasks()` method
2. Collect all tasks into plan
3. Sort by priority (so directory creation happens before file creation)

### Phase 3: Execution

```go
// internal/core/executor.go
func Execute(plan *types.Plan, tc types.TaskContext, reporter types.Reporter) error {
    for _, task := range plan.Tasks {
        reporter.TaskStart(task.Description(), current, total)
        err := task.Execute(tc)
        if err != nil {
            return err  // ← triggers transaction rollback via defer
        }
        reporter.TaskEnd(err)
    }
}
```

**What happens**:
1. For each task (in priority order):
   - Show spinner (CLI feedback)
   - Execute task (with transaction tracking)
   - Show success/failure indicator
2. If any task fails:
   - Transaction `Rollback()` is called (via defer in `RunApply`)
   - All backups are restored in LIFO order

### Phase 4: Finalization

```go
// internal/core/run.go
func executeAndFinalize(...) error {
    // 1. Execute all tasks
    err := Execute(plan, tc, reporter)
    
    // 2. Update manifest
    updateManifest(m, changes)
    
    // 3. Save manifest (atomically)
    err := manifest.Save(m, rootPath)
    
    // 4. Commit transaction (delete backups)
    err := tx.Commit()
}
```

**What happens**:
1. All tasks completed successfully
2. Update in-memory manifest with applied projects/templates
3. Atomically write `scbake.toml` (write to temp file, rename)
4. Delete backup files (point of no return)

---

## Key Design Patterns

### 1. Priority Bands

Tasks execute in bands, ensuring correct ordering:

| Band | Range | Why | Examples |
|------|-------|-----|----------|
| PrioDirCreate | 50–99 | Directories must exist before file creation | `mkdir src/` |
| PrioLangSetup | 100–999 | Language initialization | `go mod init`, `npm init` |
| PrioConfigUniversal | 1000–1099 | Config files before linting | `.editorconfig` |
| PrioCI | 1100–1199 | CI setup after config | GitHub Actions |
| PrioLinter | 1200–1399 | Linters after framework setup | ESLint config |
| PrioBuildSystem | 1400–1499 | Build tools after all setup | Makefile |
| PrioDevEnv | 1500–1999 | Development setup last | Dev container |
| PrioVersionControl | 2000–2100 | Git must be last (commits everything) | Git init |

**Implementation**: `NewPrioritySequence(base, max)` gives you the next available priority in your band.

### 2. Manifest as Source of Truth

`scbake.toml` is the **single source of truth** for project state:

```toml
[scbake]
scbake_version = "0.0.1"

[[project]]
name = "my-backend"
path = "."
language = "go"

[[project.template]]
name = "makefile"

[[project.template]]
name = "ci_github"
```

**Why this matters**:
- ✅ Reproducibility: `scbake apply` re-reads manifest
- ✅ Idempotency: Applying same templates twice is safe
- ✅ Audit trail: Git history shows what was applied, when
- ✅ Team coordination: Everyone uses same manifest

### 3. Optimistic Locking

scbake detects concurrent modifications via file modification time:

```go
// internal/manifest/io.go
func Load(startPath string) (*types.Manifest, string, error) {
    // Record modification time when file is loaded
    manifestModTimes[manifestPath] = info.ModTime()
}

func Save(m *types.Manifest, rootPath string) error {
    // Before saving, check if file was modified by another process
    originalModTime := manifestModTimes[manifestPath]
    currentModTime := os.Stat(manifestPath).ModTime()
    
    if !currentModTime.Equal(originalModTime) {
        return errors.New("manifest conflict: file was modified by another process")
    }
}
```

**Why**: Detects TOCTOU (Time-Of-Check-Time-Of-Use) bugs. If two scbake processes run simultaneously, one detects the conflict and fails.

---

## File Structure

```
scbake/
├── main.go                          # Entry point
├── cmd/                             # CLI commands
│   ├── root.go                      # Base command
│   ├── new.go                       # 'scbake new' command
│   ├── apply.go                     # 'scbake apply' command
│   └── list.go                      # 'scbake list' command
├── internal/
│   ├── core/                        # Core execution engine
│   │   ├── run.go                   # Main orchestration
│   │   └── executor.go              # Task execution loop
│   ├── filesystem/
│   │   └── transaction/             # LIFO rollback system
│   ├── manifest/                    # Manifest I/O & conflict detection
│   ├── ui/                          # CLI output (spinners, messages)
│   ├── types/                       # Core data structures
│   │   ├── plan.go                  # Handler, Task interfaces
│   │   ├── manifest.go              # Manifest structure
│   │   └── priority.go              # Priority band system
│   └── util/                        # Utilities (file ops, validation)
├── pkg/
│   ├── lang/                        # Language handlers
│   │   ├── go/
│   │   ├── spring/
│   │   └── svelte/
│   ├── templates/                   # Template handlers
│   │   ├── makefile/
│   │   ├── git/
│   │   ├── ci_github/
│   │   ├── linters/
│   │   └── ...
│   └── tasks/                       # Built-in task types
│       ├── create_template.go       # CreateTemplateTask
│       ├── exec_command.go          # ExecCommandTask
│       ├── create_directory.go      # CreateDirectoryTask
│       └── insert_xml.go            # InsertXMLTask
└── docs/                            # Documentation
    ├── EXTENDING.md                 # Extension guide
    └── ARCHITECTURE.md              # This file
```

---

## Adding a New Language Pack

To add a new language (e.g., Rust):

### 1. Create Package

```bash
mkdir -p pkg/lang/rust
touch pkg/lang/rust/rust.go
```

### 2. Implement Handler

```go
package rust

import (
    "scbake/internal/types"
    "scbake/pkg/tasks"
)

type Handler struct{}

func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
    // Create priority sequence
    seq, _ := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)
    var plan []types.Task
    
    // Add tasks
    p, _ := seq.Next()
    plan = append(plan, &tasks.ExecCommandTask{
        Cmd: "cargo",
        Args: []string{"init"},
        TaskPrio: int(p),
        RunInTarget: true,
        Desc: "Initialize Cargo project",
    })
    
    return plan, nil
}
```

### 3. Register Handler

In `pkg/lang/registry.go`:

```go
import "scbake/pkg/lang/rust"

func init() {
    Register("rust", &rust.Handler{})
}
```

### 4. Test

```bash
go build -o scbake main.go
./scbake new test-proj --lang rust
```

See `docs/EXTENDING.md` for a complete guide.

---

## Adding a New Template

Similar to languages, but use `pkg/templates/` and `PrioLinter`, `PrioBuildSystem`, etc.

Example: Add a Python linter template

```bash
mkdir -p pkg/templates/python_linter
```

Implement handler and register in `pkg/templates/registry.go`.

---

## Testing Guide

### Unit Tests

```bash
go test ./internal/types
go test ./pkg/lang/go
go test ./pkg/templates/makefile
```

### Integration Tests

```bash
go test ./tests  # Full end-to-end workflows
```

### Race Detection

```bash
go test ./... -race  # Check for race conditions
```

### Benchmarks

```bash
go test ./pkg/tasks -bench=.
```

### Code Quality

```bash
go fmt ./...           # Format code
golangci-lint run ./...# Lint
go vet ./...          # Vet checks
```

---

## Performance Considerations

### Handler Discovery (O(1))
- Handlers stored in map, lookup is constant time
- No filesystem scanning needed

### Task Execution (O(n))
- Tasks execute in sequence (single-threaded)
- No parallelization (atomic transaction requires ordering)

### Transaction System (O(n))
- Rollback is linear in number of created files
- Backups are stored efficiently (copy on write could optimize future)

### Memory Usage
- ~500KB for 50 handlers
- Depends on task plan size (typically <100 tasks per project)

---

## Security Model

### What scbake does
✅ Validates paths to prevent directory traversal
✅ Uses atomic operations to prevent partial updates
✅ Detects concurrent modification attempts
✅ Backs up files before overwrite
✅ Validates XML structure before insertion

### What scbake doesn't do
❌ Sandbox handlers (they run with user's permissions)
❌ Verify signatures (no plugin system yet)
❌ Audit all commands (could add logging in v0.2.0)
❌ Prevent fork bombs (subprocess resource limits not enforced)

**Trust model**: Handlers are trusted code. They run with user's UID.

---

## Future Improvements

### v0.1.0 (Planned)
- Config-driven extension discovery
- Built-in handler refactoring (unified system)
- Better error messages

### v0.2.0+ (Possible)
- Subprocess-based plugins (if demand exists)
- Audit logging
- Plugin registry/marketplace
- Monorepo support

---

## Contributing

1. Read this document
2. Read `CONTRIBUTING.md`
3. Follow code style: `go fmt`, `golangci-lint`
4. Add tests for new code
5. Ensure all tests pass: `go test ./...`
6. Create PR with clear description

---

## Questions?

- **How do I add a template?** See `docs/EXTENDING.md`
- **How do transactions work?** See `internal/filesystem/transaction/`
- **How is manifest versioning handled?** See `internal/manifest/io.go`
- **How are priorities resolved?** See `internal/types/priority.go`
