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
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

type Handler struct{}

func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	seq, _ := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)
	var plan []types.Task

	// Task 1: Create directory structure
	p, _ := seq.Next()
	plan = append(plan, &tasks.CreateDirectoryTask{
		Path:     "src",
		TaskPrio: int(p),
		Desc:     "Create src directory",
	})

	// Task 2: Create Cargo.toml
	p, _ = seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplatePath: "pkg/lang/rust/templates/Cargo.toml.tpl",
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
- `GetTasks(targetPath string) ([]types.Task, error)` - Returns the tasks to execute

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

scbake provides four built-in task types. Use them to compose your handler:

### 1. **CreateTemplateTask** - Create files from templates

Use this to create new files with variable substitution:

```go
plan = append(plan, &tasks.CreateTemplateTask{
	TemplatePath: "pkg/lang/rust/templates/main.rs.tpl",
	OutputPath:   "src/main.rs",
	TaskPrio:     int(p),
	Desc:         "Create main.rs",
})
```

Your template file (`main.rs.tpl`) can use Go's `text/template` syntax:

```go
// In your template:
// main.rs.tpl
fn main() {
    println!("Hello from {{.ProjectName}}");
}
```

The template receives the current `scbake.toml` manifest as context, so you can reference any fields.

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

### 3. **CreateDirectoryTask** - Create directories

Use this to create directories with full path creation:

```go
plan = append(plan, &tasks.CreateDirectoryTask{
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
	"text/template"
)

//go:embed templates/*
var templates embed.FS

type Handler struct{}

func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	seq, err := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	var plan []types.Task

	// Task 1: Create src directory
	p, _ := seq.Next()
	plan = append(plan, &tasks.CreateDirectoryTask{
		Path:     "src",
		TaskPrio: int(p),
		Desc:     "Create src directory",
	})

	// Task 2: Create Cargo.toml from template
	p, _ = seq.Next()
	cargoTmpl, err := templates.ReadFile("templates/Cargo.toml.tpl")
	if err != nil {
		return nil, fmt.Errorf("failed to read Cargo.toml template: %w", err)
	}

	plan = append(plan, &tasks.CreateTemplateTask{
		TemplatePath: string(cargoTmpl),
		OutputPath:   "Cargo.toml",
		TaskPrio:     int(p),
		Desc:         "Create Cargo.toml",
	})

	// Task 3: Create main.rs
	p, _ = seq.Next()
	mainTmpl, err := templates.ReadFile("templates/main.rs.tpl")
	if err != nil {
		return nil, fmt.Errorf("failed to read main.rs template: %w", err)
	}

	plan = append(plan, &tasks.CreateTemplateTask{
		TemplatePath: string(mainTmpl),
		OutputPath:   "src/main.rs",
		TaskPrio:     int(p),
		Desc:         "Create main.rs",
	})

	// Task 4: Initialize Cargo (optional, only if cargo is available)
	// For a production handler, you might check if `cargo` is available first
	// (See internal/preflight/preflight.go for how to do this)

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
name = "{{ .Projects | first | .Name }}"
version = "0.1.0"
edition = "2021"

[dependencies]
```

`pkg/lang/rust/templates/main.rs.tpl`:
```rust
fn main() {
    println!("Hello from {{ .Projects | first | .Name }}!");
}
```

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

If your handler fails partway through (e.g., a command not found), scbake should roll back all changes:

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
	tasks, err := h.GetTasks("/tmp/test")
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
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

type Handler struct{}

func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	seq, _ := types.NewPrioritySequence(types.PrioLinter, types.MaxLinter)
	var plan []types.Task

	p, _ := seq.Next()
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplatePath: "pkg/templates/python_linter/templates/pyproject.toml.tpl",
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

func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
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

scbake will catch this and roll back all changes.

### 5. **Embed template files**

Use Go's `embed` package to bundle templates with your handler:

```go
import "embed"

//go:embed templates/*
var templates embed.FS

// Later:
data, _ := templates.ReadFile("templates/Cargo.toml.tpl")
```

This ensures templates are included in the binary.

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
