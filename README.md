
# Scaffold Bake: The Atomic Project Scaffolder

`scbake` is a single-binary CLI tool that simplifies project setup and maintenance by applying layered infrastructure templates and language packs atomically. It uses a **manifest file (`scbake.toml`)** as the source of truth for configuration, ensuring consistency and reproducibility.


## ✨ Core Philosophy: Layering, Safety, and Extensibility

`scbake` provides a safe, composable, and customizable way to manage project infrastructure.

- **Atomic Layering and Composition**  
  Projects are built by applying independent **layers** (language packs and tooling templates). You can **mix and match** templates to achieve the desired setup.

- **Highly Configurable & Extensible**  
  Designed for flexibility. If a language pack is missing, simply **add it**. If a template doesn’t fit, you can **modify or replace it**. The handler interface simplifies extension.

- **Built-in Atomic Safety**  
  All modifications are wrapped in a **Git savepoint**. If any task fails, changes are automatically rolled back, restoring the repository to its exact previous state.

- **Prioritized Execution**  
  Tasks run in a defined order using **Priority Bands** (e.g., directory creation → language setup → universal config) to ensure dependencies are met.



## 🚀 Installation

#### (WIP)

## 📋 Commands & Usage

### `new`: Create a New Project

Creates a directory, initializes Git, sets up `scbake.toml`, and applies language packs and templates.

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

Applies new language packs or tooling templates to an existing path (requires a clean Git tree).

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
| `editorconfig`  | Universal Config (1000) | Standard file formatting                       |
| `ci_github`     | CI (1100)               | Conditional CI setup for all projects          |
| `go_linter`     | Linter (1200)           | Configures `golangci-lint`                     |
| `maven_linter`  | Linter (1200)           | Sets up Maven Checkstyle                       |
| `svelte_linter` | Linter (1200)           | Configures ESLint 9 with Svelte rules          |
| `makefile`      | Build System (1400)     | Universal build/lint scripts for all projects  |
| `devcontainer`  | Dev Env (1500)          | Auto-detects languages and installs toolchains |


## 💻 Extending `scbake`

1. Create a new package under `pkg/lang` or `pkg/templates`.
2. Implement the `Handler` interface using task types (`CreateTemplateTask`, `ExecCommandTask`, etc.).
3. Register the handler in the relevant `registry.go`.


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
| `PrioDevEnv`          | 1500+     | Dev environment setup |


### Global Flags

| Flag              | Description                                |
| :---------------- | :----------------------------------------- |
| `--dry-run`       | Show planned changes without applying them |
| `--force`         | Override safety checks                     |
| `-v`, `--version` | Show version (`v0.0.1-dev`)                |
