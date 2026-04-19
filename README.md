# Scaffold Bake: The Atomic Project Scaffolder

`scbake` is a single-binary CLI tool that simplifies project setup and maintenance by applying layered infrastructure templates and language packs atomically. It uses a **manifest file (`scbake.toml`)** as the source of truth for configuration, ensuring consistency and reproducibility.


## Core Philosophy: Layering, Safety, and Extensibility

`scbake` provides a safe, composable, and customizable way to manage project infrastructure.

- **Atomic Layering and Composition**  
  Projects are built by applying independent **layers** (language packs and tooling templates). You can **mix and match** templates to achieve the desired setup.

- **Highly Configurable & Extensible**  
  Designed for flexibility. If a language pack is missing, simply **add it**. If a template doesn't fit, you can **modify or replace it**. The handler interface simplifies extension.

- **Built-in Atomic Safety**  
  All modifications are managed by a **LIFO-based Transaction Manager**. If any task fails, the engine executes a journaled rollback. This deletes created artifacts and restores file backups in reverse order, ensuring the filesystem returns to its original state.

- **Prioritized Execution**  
  Tasks run in a defined order using **Priority Bands** (e.g., directory creation → language setup → universal config) to ensure dependencies are met.



## Installation

### Quick Install (macOS, Linux, Windows)

```bash
curl -sSL https://raw.githubusercontent.com/Emin-ACIKGOZ/scbake/master/install.sh | bash
```

Or with `wget`:
```bash
wget -qO- https://raw.githubusercontent.com/Emin-ACIKGOZ/scbake/master/install.sh | bash
```

The installer detects your OS/architecture and installs the latest binary to your PATH.

**See [INSTALLING.md](docs/INSTALLING.md)** for detailed instructions, troubleshooting, and alternative installation methods:
- Go Install
- Manual download from GitHub Releases
- Build from source

### Build from Source

To compile the binary yourself, ensure you have **Go 1.21+** installed:

#### 1. Clone the repository:
```bash
git clone https://github.com/Emin-ACIKGOZ/scbake.git
cd scbake
```

#### 2. Build the project:
```bash
go build -o scbake main.go
```

#### 3. Move the binary to your path (optional):
```bash
mv scbake /usr/local/bin/
```

## Commands & Usage

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


## Supported Language Packs

| Language   | Initialization Tasks                                                            | Required Binaries       |
| :--------- | :------------------------------------------------------------------------------ | :---------------------- |
| **Go**     | Creates `.gitignore`, `main.go`; runs `go mod init`, `go mod tidy`              | `go`                    |
| **Svelte** | Runs `npm create vite@latest`, installs dependencies, sets NPM scripts          | `npm`                   |
| **Spring** | Downloads starter zip from `start.spring.io`, extracts, makes `mvnw` executable | `curl`, `unzip`, `java` |


## Tooling Templates

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

## Extending `scbake`

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


## Development Details

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
