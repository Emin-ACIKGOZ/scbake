
# Scaffold Bake: The Atomic Project Scaffolder

**Create production-ready projects in 3 minutes.** `scbake` is a single-binary CLI that scaffolds complete project structures with language setup, CI/CD, linters, and dev environments—all atomically, with automatic rollback on failure.

```bash
scbake new my-backend --lang go --with makefile,ci_github
cd my-backend
# ✅ You have: Go project, Makefile, GitHub Actions, scbake.toml
```

No configuration files. No templates to maintain. No partial failures. Everything works the first time.

---

## Why scbake?

**Before scbake**: New projects need 30+ manual steps (create dirs, init language, add CI, linters, Makefiles, dev containers...). Easy to miss steps, hard to standardize across teams.

**With scbake**: `scbake new` → your project is ready. Same setup everywhere. If anything fails, everything rolls back.

### ✨ Core Features

- **Atomic Operations** - All changes apply transactionally. If a step fails, scbake rolls back everything. No partial, broken projects.
- **3 Language Packs** - Go, Spring (Java), Svelte (Frontend). Add custom languages by forking.
- **8 Built-in Templates** - Git, CI/CD, linters, Makefile, dev containers, editor config.
- **One Manifest** - `scbake.toml` is your source of truth. Reproducible everywhere.
- **No Dependencies** - Pure Go, standard library only. Single binary, no setup friction.
- **Easily Extensible** - Fork scbake, add handlers, compile. Not a plugin system—just Go code.



## 🚀 Installation

`scbake` is currently in **alpha development**. There is no fixed method for installation or distribution beyond compiling from source at this stage.

### Build from Source

To compile the binary yourself, ensure you have **Go 1.21+** installed:

#### 1. Clone the repository:
```bash
git clone https://github.com/Emin-ACIKGOZ/scbake.git
```


#### 2. Build the project:
```bash
go build -o scbake main.go
```


#### 3. Move the binary to your path (optional):
```bash
mv scbake /usr/local/bin/
```

## 📋 Commands & Usage

### `new`: Create a New Project

Creates a new directory, bootstraps the `scbake.toml` manifest, and applies language packs and templates. 

**Note:** Git initialization can be added via the `--with git` template.

```bash
scbake new <project-name> [--lang <lang>] [--with <template...>]
```

| Flag     | Description                                      | Example                     |
| :------- | :----------------------------------------------- | :-------------------------- |
| `--lang` | Primary language pack (`go`, `svelte`, `spring`) | `--lang go`                 |
| `--with` | Comma-separated tooling templates                | `--with makefile,ci_github` |

**Example:**

```bash
scbake new my-backend --lang go --with makefile,ci_github
```


### `apply`: Apply Templates to an Existing Project

Applies new language packs or tooling templates to an existing path. Because `scbake` uses its own transaction logic, it does **not** require a clean Git tree to operate safely.

```bash
scbake apply [--lang <lang>] [--with <template...>] [<path>]
```

| Argument | Description      | Default |
| :------- | :--------------- | :------ |
| `<path>` | Target directory | `.`     |

**Example:**

```bash
scbake apply --with maven_linter
```


### `list`: View Available Resources

Lists available or applied resources.

```bash
scbake list [langs|templates|projects]
```


## 🌐 Supported Language Packs

| Language   | Initialization Tasks                                                            | Required Binaries       |
| :--------- | :------------------------------------------------------------------------------ | :---------------------- |
| **Go**     | Creates `.gitignore`, `main.go`; runs `go mod init`, `go mod tidy`              | `go`                    |
| **Svelte** | Runs `npm create vite@latest`, installs dependencies, sets NPM scripts          | `npm`                   |
| **Spring** | Downloads starter zip from `start.spring.io`, extracts, makes `mvnw` executable | `curl`, `unzip`, `java` |


## 🛠️ Tooling Templates

| Template        | Priority Band           | Features                                       |
| :-------------- | :---------------------- | :--------------------------------------------- |
| `editorconfig`  | Universal Config (1000) | Standard file formatting across the project                       |
| `ci_github`     | CI (1100)               | Conditional CI setup based on detected languages          |
| `go_linter`     | Linter (1200)           | Standard `golangci-lint` configuration                     |
| `maven_linter`  | Linter (1200)           | Checkstyle config + automatic pom.xml plugin integration                       |
| `svelte_linter` | Linter (1200)           | ESLint 9 integration for Svelte projects          |
| `makefile`      | Build System (1400)     | Universal build/lint scripts for all projects  |
| `devcontainer`  | Dev Env (1500)          | Containerized DX with auto-detected toolchains |
| `git`  | Version Control (2000)          | Initializes repo, stages all files, and creates initial commit |

## 💻 Extending `scbake`

### Creating Custom Handlers

1. Create a new package under `pkg/lang` or `pkg/templates`.
2. Implement the `Handler` interface using task types.
3. Register the handler in the relevant `registry.go`.

### Available Task Types

- **CreateTemplateTask**: Creates new files from embedded templates (with manifest data available)
- **ExecCommandTask**: Executes shell commands with optional output tracking
- **CreateDirectoryTask**: Creates directories with transaction tracking
- **InsertXMLTask**: Modifies existing XML files by inserting fragments at specified paths

### Example: Modifying Existing XML Files

For handlers that need to modify existing XML files (like Maven pom.xml), use `InsertXMLTask`:

```go
// Read snippet from embedded template
snippet, _ := templates.ReadFile("plugin_snippet.xml.tpl")

// Create insert task
task := &tasks.InsertXMLTask{
    FilePath:    "pom.xml",
    ElementPath: "/project/build/plugins",
    XMLContent:  string(snippet),
    Desc:        "Inject plugin into pom.xml",
    TaskPrio:    1201,
}

// The task automatically:
// - Validates XML structure
// - Prevents duplicate insertions (idempotent)
// - Integrates with transaction manager for rollback
// - Validates paths to prevent directory traversal
```


## ⚙️ Development Details

### Task Execution Priority

| Band Name             | Range     | Purpose               |
| :-------------------- | :-------- | :-------------------- |
| `PrioDirCreate`       | 50–99     | Directory creation    |
| `PrioLangSetup`       | 100–999   | Language setup        |
| `PrioConfigUniversal` | 1000–1099 | Universal config      |
| `PrioCI`              | 1100–1199 | CI workflows          |
| `PrioLinter`          | 1200–1399 | Linter setup          |
| `PrioBuildSystem`     | 1400–1499 | Build systems         |
| `PrioDevEnv`          | 1500-1999     | Dev environment setup |
| `PrioVersionControl`          | 2000-2100     | VCS initialization (Git) |

### Global Flags

| Flag              | Description                                |
| :---------------- | :----------------------------------------- |
| `--dry-run`       | Show planned changes without applying them |
| `--force`         | Override safety checks                     |
| `-v`, `--version` | Show version (`v0.0.1-dev`)                |
