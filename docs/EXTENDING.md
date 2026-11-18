# Extending scbake: The Complete Guide

scbake is designed to be easily extended. You can add custom languages, templates, or build your own version tailored to your organization.

## The Fastest Way: Fork & Modify

scbake's extension system is simple: **fork the repository, add your handler, compile.**

No complex plugins, no manifests, no subprocess communication. Just Go code and the existing task system.

### Step 1: Clone & Build

```bash
git clone https://github.com/Emin-ACIKGOZ/scbake.git
cd scbake
go build -o scbake main.go
```

You now have a working scbake binary.

### Step 2: Add Your Handler

Create a new package under `pkg/lang/` (for languages) or `pkg/templates/` (for tooling):

```bash
mkdir -p pkg/lang/rust
touch pkg/lang/rust/rust.go
```

### Step 3: Copy the Template

Create your handler by implementing the `Handler` interface. Here's the simplest possible example:

```go
// pkg/lang/rust/rust.go
package rust

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed templates/*
var templates embed.FS

type Handler struct{}

func (h *Handler) GetTasks(_ string, _ string, _ string, _ map[string]string) ([]types.Task, error) {
	seq, _ := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)
	var plan []types.Task

	// Task 1: Create directory structure
	p, _ := seq.Next()
	plan = append(plan, &tasks.CreateDirTask{
		Path:     "src",
		TaskPrio: int(p),
		Desc:     "Create src directory",
	})

	// Task 2: Create Cargo.toml
	p, _ = seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "templates/Cargo.toml.tpl",
		OutputPath:   "Cargo.toml",
		TaskPrio:     int(p),
		Desc:         "Create Cargo.toml",
	})

	// Task 3: Initialize Rust project
	p, _ = seq.Next()
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "cargo",
		Args:        []string{"init", "--name", "my-rust-app"},
		TaskPrio:    int(p),
		RunInTarget: true,
		Desc:        "Initialize Rust project",
	})

	return plan, nil
}
```

That's it. Your handler is a single Go struct that implements two methods:
- `GetTasks(targetPath string, templateDir string, registryCacheDir string, vars map[string]string) ([]types.Task, error)` - Returns the tasks to execute. `vars` carries only the explicit `--set` values (see [Consuming variables](#consuming-variables)).

If your handler reads template files **while building the plan** (instead of
creating `CreateTemplateTask`, which resolves at execution time), implement the
optional `types.RegistryAwareHandler` interface so those lookups honor registry
overrides; without it, core falls back to `GetTasks` and the registry tier is
disabled for that lookup:

```go
func (h *Handler) GetTasksWithRegistries(targetPath, templateDir, registryCacheDir string, registries []string) ([]types.Task, error) {
    // Use `registries` for every plan-time template lookup.
    content, err := tasks.ReadTemplate(fs, "snippet.tpl", templateDir, registryCacheDir, "my_template", registries)
    // ...
}
```

Core calls `GetTasksWithRegistries` when a handler implements it and falls back
to `GetTasks` otherwise. Pass the allowlist through to `tasks.ReadTemplate`;
nil or empty means the registry tier is disabled (fail closed), never "search
every cache directory". See `pkg/templates/maven_linter` for an example.

To disclose the resolved source tier in `--dry-run`, implement
`DescriptionWithSource(tc types.TaskContext) string` (the `types.SourceDescriber`
interface) on the task. Built-in `CreateTemplateTask` already does.

### Step 4: Register Your Handler

Add one line to register your handler in `pkg/lang/registry.go`:

```go
// pkg/lang/registry.go (add this line in the init() function)
Register("rust", &rust.Handler{})
```

### Step 5: Compile & Test

```bash
go build -o scbake main.go
./scbake new my-rust-app --lang rust
cd my-rust-app
cat scbake.toml  # Verify your handler ran
```

That's the complete flow. No manifest files, no subprocess calls, no complex protocols.

---

## Understanding Priority Bands

scbake executes tasks in priority order. Different task types belong to different "bands":

| Band | Range | Purpose | Example |
|------|-------|---------|---------|
| **PrioDirCreate** | 50–99 | Create directories | `mkdir src/` |
| **PrioLangSetup** | 100–999 | Language initialization | `npm init`, `cargo init` |
| **PrioConfigUniversal** | 1000–1099 | Project-wide config | `.editorconfig`, `.gitignore` |
| **PrioCI** | 1100–1199 | CI/CD setup | GitHub Actions, GitLab CI |
| **PrioLinter** | 1200–1399 | Linters & formatters | ESLint, golangci-lint |
| **PrioBuildSystem** | 1400–1499 | Build tools | Makefile, build scripts |
| **PrioDevEnv** | 1500–1999 | Development environment | Dev containers, IDE config |
| **PrioVersionControl** | 2000–2100 | VCS initialization | Git init, first commit |

**Why this matters**: If you're adding a language handler, use `PrioLangSetup` (100–999). If you're adding a linter template, use `PrioLinter` (1200–1399).

**How to use it in your handler**:

```go
// Create a sequence within your band
seq, _ := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)

// Get the next priority in your band
p, _ := seq.Next()  // First call returns ~100
p, _ := seq.Next()  // Second call returns ~101
```

Each call to `seq.Next()` gives you the next available priority in your band. Tasks execute in priority order, so your band's tasks run before linters (1200+) and after config setup (1000–1099).

---

## Available Task Types

The `pkg/tasks` package provides six reusable task types. Use them to compose your handler:

### 1. **CreateTemplateTask** - Create files from templates

Use this to create new files with variable substitution:

```go
plan = append(plan, &tasks.CreateTemplateTask{
	TemplateFS:   templates,
	TemplatePath: "templates/main.rs.tpl",
	OutputPath:   "src/main.rs",
	TaskPrio:     int(p),
	Desc:         "Create main.rs",
})
```

Your template file (`main.rs.tpl`) can use Go's `text/template` syntax:

```gotemplate
// main.rs.tpl
fn main() {
    println!("Hello from {{ .Project.Name }}");
}
```

The template receives a render-data wrapper around the current `scbake.toml`.
Every manifest field is available directly (`.Projects`, `.Metadata`,
`.SbakeVersion`, ...), plus an additive project-scoped field:

- `.Project` — the manifest project whose `path` owns the directory the task is
  writing into. Prefer `.Project.Name` over `(index .Projects 0).Name`: the
  latter always names the first project in the manifest, which is wrong in a
  monorepo.

`.Project` is nil when no `[[projects]]` entry matches the task's target
directory (for example, a monorepo root when no project declares the root
path), or when execution has no transaction root. A root project with
`path = "."` resolves normally. Guard `.Project` and choose the fallback
deliberately. To keep the historical first-project behavior:

```gotemplate
# Contributing to {{ if .Project }}{{ .Project.Name }}{{ else if .Projects }}{{ (index .Projects 0).Name }}{{ else }}this project{{ end }}
```

Use `.Projects` only for legacy compatibility or when you intentionally need
the full manifest list.

### 2. **ExecCommandTask** - Run shell commands

Use this to execute commands:

```go
plan = append(plan, &tasks.ExecCommandTask{
	Cmd:         "cargo",
	Args:        []string{"init"},
	TaskPrio:    int(p),
	RunInTarget: true,
	Desc:        "Initialize Cargo project",
})
```

**Important fields**:
- `RunInTarget: true` - Run in the project directory (not scbake's directory)
- `Cmd` and `Args` - The command to execute (no shell interpretation, so it's safe)
- `PredictedCreated` - Files or directories this command may create or modify. Missing regular files are snapshotted after the command and removed on rollback only when their bytes still match. Existing regular files are restored with their content and mode. A missing directory is removed on rollback only when empty, so a directory populated by the command is kept as residue; declare individual files rather than a parent directory. Existing directories are not snapshotted.
- `ArchivePath` - Optional zip archive whose entries, and any missing ancestor directories, are tracked before extraction.

Commands that write outside declared paths, pre-existing dependency trees such
as `node_modules`, and tool caches remain outside the transaction.

### 3. **CreateDirTask** - Create directories

Use this to create directories with full path creation:

```go
plan = append(plan, &tasks.CreateDirTask{
	Path:     "src",
	TaskPrio: int(p),
	Desc:     "Create src directory",
})
```

### 4. **InsertXMLTask** - Modify XML files

Use this to insert XML fragments into existing files (e.g., Maven pom.xml):

```go
plan = append(plan, &tasks.InsertXMLTask{
	FilePath:    "pom.xml",
	ElementPath: "/project/build/plugins",
	XMLContent:  "<plugin>...</plugin>",
	TaskPrio:    int(p),
	Desc:        "Add plugin to pom.xml",
})
```

This task:
- ✅ Automatically detects and prevents duplicate insertions
- ✅ Validates XML structure before modifying
- ✅ Integrates with the transaction system for rollback
- ✅ Validates paths to prevent directory traversal attacks

### 5. **AppendFileTask** - Append to existing files

Use this to add content to a file that may already exist. The task creates the file when
it is missing and skips the write when the content is already present as a standalone
block, so it is safe to re-run. The parent directory must already exist (the task does not
create missing ancestors), so create it first (for example with `CreateDirTask`) when it
is absent:

```go
plan = append(plan, &tasks.AppendFileTask{
	FilePath: ".github/CODEOWNERS",
	Content:  "# Managed by scbake\n* @maintainers\n",
	TaskPrio: int(p),
	Desc:     "Initialize .github/CODEOWNERS",
})
```

This task:
- ✅ Creates the file when it does not exist
- ✅ Skips the append when the content is already present as a standalone block
- ✅ Integrates with the transaction system for rollback
- ✅ Validates paths to prevent directory traversal attacks

### 6. **ScaffoldTask** - Stage a project generator

Use this for generators that refuse to run in a non-empty directory. The task
runs the command in an empty staging leaf under the transaction temp directory,
checks a sentinel file, and moves the generated top-level entries into the
target:

```go
plan = append(plan, &tasks.ScaffoldTask{
	Cmd:      "npm",
	Args:     []string{"create", "vite@latest", ".", "--", "--template", "svelte"},
	TaskPrio: int(p),
	Desc:     "Scaffold Svelte project",
	Sentinel: "package.json",
})
```

**Important fields**:
- `Cmd` and `Args` - The generator command; no shell interpretation.
- `Sentinel` - A path relative to the staging leaf that must exist as a regular
  file after the command. It catches generators that exit successfully after
  canceling without writing anything.
- `TaskPrio` and `Desc` - The usual task ordering and progress description.

The staging leaf is named after the resolved (symlink-free) target basename so
generators that derive a package name from the working directory keep the
expected name. The task preflights existing top-level destinations and fails
before moving anything instead of overwriting them.

---

## Complete Example: Rust Language Handler

Here's a full, working Rust handler you can copy:

```go
// pkg/lang/rust/rust.go
package rust

import (
	"embed"
	"fmt"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed templates/*
var templates embed.FS

type Handler struct{}

func (h *Handler) GetTasks(_ string, _ string, _ string, _ map[string]string) ([]types.Task, error) {
	seq, err := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	var plan []types.Task

	// Task 1: Create src directory
	p, _ := seq.Next()
	plan = append(plan, &tasks.CreateDirTask{
		Path:     "src",
		TaskPrio: int(p),
		Desc:     "Create src directory",
	})

	// Task 2: Create Cargo.toml from template
	p, _ = seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "templates/Cargo.toml.tpl",
		OutputPath:   "Cargo.toml",
		TaskPrio:     int(p),
		Desc:         "Create Cargo.toml",
	})

	// Task 3: Create main.rs
	p, _ = seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "templates/main.rs.tpl",
		OutputPath:   "src/main.rs",
		TaskPrio:     int(p),
		Desc:         "Create main.rs",
	})

	return plan, nil
}
```

**Create the templates**:

```bash
mkdir -p pkg/lang/rust/templates
```

`pkg/lang/rust/templates/Cargo.toml.tpl`:
```toml
[package]
name = "{{ .Project.Name }}"
version = "0.1.0"
edition = "2021"

[dependencies]
```

`pkg/lang/rust/templates/main.rs.tpl`:
```rust
fn main() {
    println!("Hello from {{ .Project.Name }}!");
}
```

`.Project` is set when the task writes into a directory that is a manifest
project. For templates that may also run at the repository root or outside a
listed project, guard it with the fallback shown in
[CreateTemplateTask](#1-createtemplatetask---create-files-from-templates).

**Register it**:

Add to `pkg/lang/registry.go`:
```go
func init() {
	// ... existing registrations
	Register("rust", &rust.Handler{})
}
```

**Compile & test**:

```bash
go build -o scbake main.go
./scbake new my-app --lang rust
cd my-app
cat Cargo.toml  # Verify it works
```

---

## Declaring a Schema (Input Validation)

Each template can declare its expected inputs in a `schema.json` embedded alongside its templates. scbake validates the manifest metadata and `--set` flags against all selected templates' schemas before executing a single task.

### 1. Create `schema.json`

Place it in your handler package:

```json
{
  "description": "What this template does",
  "variables": {
    "oncall_alias": {
      "type": "string",
      "required": true,
      "description": "PagerDuty on-call alias"
    },
    "service_id": {
      "type": "string",
      "required": true,
      "description": "Internal service identifier"
    },
    "team_name": {
      "type": "string",
      "required": false,
      "default": "platform",
      "description": "Team responsible for this service"
    }
  }
}
```

### 2. Embed the schema

Update your `//go:embed` directive and implement `SchemaProvider`:

```go
//go:embed templates/* schema.json
var myTemplates embed.FS

type Handler struct{}

func (h *Handler) SchemaFS() embed.FS   { return myTemplates }
func (h *Handler) SchemaPath() string   { return "schema.json" }

func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
    // ...
}
```

### 3. Provide values via `--set`

Users pass variable values on the command line:

```bash
scbake new my-service --with my_template \
  --set oncall_alias=my-team \
  --set service_id=srv-abc123
```

If a required variable is missing, scbake fails immediately with a clear message:

```
Error: input validation failed:
template "my_template":
schema validation failed with 1 error(s):
  - variable "oncall_alias": required variable "oncall_alias" is missing
```

### Consuming variables

Declaring a variable in `schema.json` makes it validatable and documents it; it
does not by itself change generated output or record a value. Only a supplied
value (`--set`, or `--license`/`--copyright-holder`) or a schema `default` is
written to `[metadata]`. A variable only affects output when the handler that
owns it reads it:

- **Language handlers** receive the explicit `--set` values as the `vars`
  argument of `GetTasks`. `vars` never contains schema defaults, may be `nil`,
  and a missing or empty value means "no override". A handler's *variable
  handling* must be a no-op when `vars` is empty, because `update` and ordinary
  `new`/`apply` runs pass no values; it still performs its normal setup, and it
  must ignore `--set` keys owned by other selected handlers.
- **Template handlers** read inputs from `.Metadata` while rendering, where both
  explicit values and schema defaults are available.

For **language handlers**, values are consumed only when passed on the current
invocation: a value already recorded in `scbake.toml` is not re-applied by a
later `apply` unless `--set` is given again. This keeps schema defaults (which
are persisted in `[metadata]`) from silently overriding toolchain or project
state.

**Template handlers** are different: every render re-reads the effective inputs
from `.Metadata` (persisted explicit values plus schema defaults), so a later
`update` rewrites the output to match the recorded values for a managed file
with no local edits. With the default `merge` strategy, local edits are kept or
merged into the new render without an artifact; an artifact appears only when
the local file cannot be reconciled safely (a conflicting local edit, a
missing or stale merge base, or a file scbake did not previously manage) or when
`--conflict-strategy artifact` is used. For example, editing
`runner_os` in `[metadata]` and running `update` rewrites `runs-on`.

The Go language pack consumes `module_path` (new modules only; an existing
module path is never rewritten — a differing explicit value is an error) and
`go_version` (new modules and explicitly updated ones). Existing `go.mod`
tokens are compared after unquoting a double-quoted path; a backtick-quoted
directive is invalid `go.mod` and is rejected. An explicit version is also
passed to `go mod tidy -go=<version>`, so on that run a request lower than a
dependency's effective Go requirement fails the run instead of leaving `go.mod`
and the metadata recorded by that run disagreeing. Only a run that passes
`--set go_version` is fail-closed: a later `apply` without `--set` runs plain
`go mod tidy`, which can raise the directive and leave the recorded version
stale. The comparison includes the patch component: a dependency that requires
`go 1.24.0` is not satisfied by a requested `1.24`, so name the full version
(`1.24.0`) when the requirement carries a patch. (The project's own `go`
directive is not a blocker; `tidy -go=1.24` lowers a main-module `go 1.24.0` to
`1.24`.)
The `ci_github` template consumes `runner_os` at render time and emits
`shell: bash` for the Windows runner, whose default shell is PowerShell. It also
consumes `go_version` when set with `--set`, rendering that value as the CI
pin; when absent it renders `go-version-file` for the first Go project's
`go.mod`, so CI uses the same directive the Go pack wrote.

Manifests written by older versions may still record `go_version` (it was
formerly a schema default). That recorded value keeps rendering as an explicit
`go-version` pin after `update`, even when it differs from `go.mod`; there is no
automatic migration. Remove the `go_version` entry from `[metadata]` to switch
CI to `go-version-file`, or re-apply the Go pack with `--set go_version=<version>`
to make the explicit pin match `go.mod`. Re-applying the Go pack rewrites the
`go` directive in `go.mod` to that version but does not re-render the workflow;
the already-rendered workflow picks up the recorded value on a later `update`
or `apply --with ci_github`. `build_tool` in the
`makefile` template is the documented record-only example: it is validated and
stored but intentionally has no consumer.

On a failed `apply --lang go` against a project without `go.mod`, the first
content snapshot of the created `go.mod` is authoritative, so a later command
write (for example a version edit before a failing tidy) makes rollback keep the
file with a warning rather than delete bytes it cannot prove it wrote. The
residue is a new `go.mod`; `new` normally avoids it because a failed `new`
removes the whole project directory, though a lock-busy failure leaves the
bootstrap residue. To recover, repair the leftover directive and rerun `apply`:
use `GOTOOLCHAIN=local go mod edit -go=<valid version>`, edit the `go` line in
`go.mod` by hand, or delete and recreate `go.mod`. `GOTOOLCHAIN=local` is needed
because under the default `GOTOOLCHAIN=auto`, a residue such as `go 1.999` makes
even `go mod edit` fail while it tries to download that toolchain. Plain
`go mod tidy` only works once the directive is satisfiable. See
[ARCHITECTURE.md](ARCHITECTURE.md) for the transaction rule.

### Schema Reference

| Field        | Values                     | Description                          |
| :----------- | :------------------------- | :----------------------------------- |
| `type`       | `string`, `number`, `boolean` | Expected input type               |
| `required`   | `true`, `false`            | Must be provided by the user         |
| `default`    | any string                 | Used when the user omits the value; must satisfy `type`, `pattern`, and `enum` or validation fails |
| `description`| text                       | Human-readable documentation         |

---

## Testing Your Handler

Once you've created your handler, test it thoroughly:

### 1. **Test with --dry-run**

```bash
scbake new test-proj --lang rust --dry-run
```

This shows what would happen without actually creating files.

### 2. **Test the full workflow**

```bash
scbake new test-proj --lang rust
cd test-proj
cat scbake.toml  # Check that your handler is registered
```

### 3. **Test rollback**

If your handler fails partway through (e.g., a command not found), scbake should roll back all tracked changes:

```bash
# Create a handler that fails intentionally
scbake new test-proj --lang rust
# Interrupt or cause an error
# The project directory should be cleaned up
```

### 4. **Unit testing** (advanced)

Write Go tests for your handler:

```go
// pkg/lang/rust/rust_test.go
package rust

import (
	"testing"
)

func TestHandler_GetTasks(t *testing.T) {
	h := &Handler{}
	tasks, err := h.GetTasks("/tmp/test", "", "", nil)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("Expected at least one task")
	}
}
```

Run with:
```bash
go test ./pkg/lang/rust
```

---

## Adding a Template Handler

The same pattern applies to templates. For example, to add a Python linter template:

```bash
mkdir -p pkg/templates/python_linter
touch pkg/templates/python_linter/python_linter.go
```

```go
// pkg/templates/python_linter/python_linter.go
package python_linter

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed templates/*
var templates embed.FS

type Handler struct{}

func (h *Handler) GetTasks(_ string, _ string, _ string) ([]types.Task, error) {
	seq, _ := types.NewPrioritySequence(types.PrioLinter, types.MaxLinter)
	var plan []types.Task

	p, _ := seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "templates/pyproject.toml.tpl",
		OutputPath:   "pyproject.toml",
		TaskPrio:     int(p),
		Desc:         "Create pyproject.toml with linting config",
	})

	return plan, nil
}
```

Register in `pkg/templates/registry.go`:
```go
Register("python_linter", &python_linter.Handler{})
```

---

## Best Practices

### 1. **Make handlers idempotent**

If a user runs the same handler twice, it should be safe:

```go
// Good: CreateTemplateTask overwrites by default, InsertXMLTask checks for duplicates
plan = append(plan, &tasks.CreateTemplateTask{
	TemplatePath: "...",
	OutputPath:   "...",
	// If the file exists, it's overwritten (idempotent)
})

// Good: InsertXMLTask prevents duplicate insertions automatically
plan = append(plan, &tasks.InsertXMLTask{
	XMLContent: "...",
	// Won't insert the same XML twice
})

// Good: ExecCommandTask should be idempotent if possible
plan = append(plan, &tasks.ExecCommandTask{
	Cmd: "go",
	Args: []string{"mod", "tidy"},  // Running twice is safe
})
```

### 2. **Check for required binaries**

If your handler requires a binary (like `cargo` or `npm`), check for it:

```go
import "scbake/internal/preflight"

func (h *Handler) GetTasks(_ string, _ string, _ string, _ map[string]string) ([]types.Task, error) {
	if err := preflight.CheckBinaries("cargo"); err != nil {
		return nil, fmt.Errorf("cargo not found: %w", err)
	}
	// ... continue with tasks
}
```

### 3. **Use meaningful descriptions**

Descriptions appear in the output. Make them clear:

```go
Desc: "Create Cargo.toml"         // Good
Desc: "cargo"                       // Bad
Desc: "Create config file template" // Vague
```

### 4. **Return errors gracefully**

If something goes wrong, return a clear error:

```go
if err != nil {
	return nil, fmt.Errorf("failed to create priority sequence: %w", err)
}
```

scbake will catch this and roll back all tracked changes.

### 5. **Embed template files**

Use Go's `embed` package to bundle templates with your handler:

```go
import "embed"

//go:embed templates/*
var templates embed.FS

// Later:
data, _ := templates.ReadFile("templates/Cargo.toml.tpl")
```

Then pass `TemplateFS` to `CreateTemplateTask` so the template is resolved through the full resolution chain (override dir → configured registry cache → embedded).

### Migration: `tasks.ReadTemplate`

`ReadTemplate` now takes the configured registry allowlist as its final
argument. Nil or empty disables the registry tier (fail closed).

```go
// before
content, err := tasks.ReadTemplate(fs, path, dir, cache, name)
// after (execution-time path; tc is the types.TaskContext)
content, err := tasks.ReadTemplate(fs, path, dir, cache, name, tc.RegistryNames)
// plan-time handler: implement GetTasksWithRegistries and pass `registries`
```

---

## Organization Structure

When your extension grows, organize it clearly:

```
scbake/
├── pkg/
│   └── lang/
│       └── rust/
│           ├── rust.go             # Handler implementation
│           ├── rust_test.go        # Tests
│           ├── templates/
│           │   ├── Cargo.toml.tpl
│           │   └── main.rs.tpl
│           └── README.md           # Handler-specific docs
```

---

## Contributing Your Handler Back

If your handler is useful, consider contributing it back to scbake:

1. Fork scbake on GitHub
2. Add your handler to `pkg/lang/` or `pkg/templates/`
3. Add tests
4. Create a pull request
5. Describe your use case in the PR

We accept handlers that:
- ✅ Work reliably
- ✅ Have tests
- ✅ Follow the code style (go fmt, golangci-lint)
- ✅ Solve a real problem

---

## Troubleshooting

**Q: "command not found" when running my handler**

A: Make sure the binary is in your PATH:
```bash
which cargo  # Or: which npm, go, etc.
```

**Q: "template file not found"**

A: Verify the path is relative to your handler package:
```bash
# If your handler is pkg/lang/rust/rust.go
# Your template should be at pkg/lang/rust/templates/Cargo.toml.tpl
```

**Q: Changes rolled back unexpectedly**

A: Check scbake's output—it will show which task failed. Fix that task and try again.

**Q: How do I debug my handler?**

A: Use `--dry-run` to see what would happen:
```bash
scbake new test --lang rust --dry-run
```

And use `go run main.go` instead of the compiled binary during development:
```bash
go run main.go new test --lang rust
```

---

## Next Steps

1. **Fork scbake** on GitHub
2. **Create your handler** (language or template)
3. **Test it** with `--dry-run` and a real project
4. **Use it** in your organization
5. **Share it** - consider opening a PR to contribute back

Happy extending!
