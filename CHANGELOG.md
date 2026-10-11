# Changelog

All notable changes to scbake are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
## [0.5.12] - 2026-10-11

### Bug Fixes

- **ci:** Pass workflow_dispatch tag via env

## [0.5.11] - 2026-10-11

### Bug Fixes

- **transaction:** Sweep stale transaction dirs and ignore .scbake

## [0.5.10] - 2026-10-10

### Bug Fixes

- **tasks:** Scope managed_files keys by project root

## [0.5.9] - 2026-10-10

### Bug Fixes

- **registry:** Keep download context alive and redact credentials

## [0.5.8] - 2026-10-10

### Bug Fixes

- **tasks:** Track created dirs on rollback and correct the artifact ledger

## [0.5.7] - 2026-10-10

### Bug Fixes

- **tasks:** Make append idempotency line-aware

## [0.5.6] - 2026-10-10

### Bug Fixes

- **transaction:** Accept dot-prefixed names and resolve relative paths against root

## [0.5.5] - 2026-10-10

### Bug Fixes

- **manifest:** Default scbake_version for empty manifests (#67)

## [0.5.4] - 2026-10-10

### Bug Fixes

- **cli:** Register apply and list exactly once (#66)

### Continuous Integration

- Pin the macOS test jobs to an unaffected runner image (#16)

## [0.5.3] - 2026-10-10

### Bug Fixes

- **fileutil:** Reject foreign-absolute paths in SafePath (#17)

### Continuous Integration

- Test on Linux, macOS, and Windows and enforce a coverage gate
- Enforce Conventional Commits on pull requests
- Automate releases with svu, git-cliff, and goreleaser
- Validate pull request titles for squash merges
- Authenticate release pushes with a deploy key (#4)
- **release:** Guard against re-tagging and fix changelog output (#5)
- **release:** Support re-releasing an existing tag (#6)
- **release:** Re-release tags with the current goreleaser config (#7)
- **release:** Keep git state clean during goreleaser re-releases (#8)
- **release:** Use a supported template for the windows executable name (#9)
- **release:** Drop the manual .exe suffix for windows binaries (#10)

### Documentation

- **governance:** Add AI usage policy
- **scope:** Define core scope and extension boundary
- **triage:** Add issue triage guide
- **commit:** Add commit message conventions
- **contributing:** Restructure guide and add maintainer scaffolding
- **releasing:** Correct branch references to master
- **commit:** Note the enforced header length limit
- **contributing:** Require DCO sign-off
- **releasing:** Document the automated release process
- Drop stale version literal from the README

### Miscellaneous Tasks

- **github:** Replace markdown issue templates with issue forms
- **license:** Relicense scbake to Apache-2.0

### Testing

- **tasks:** Skip the POSIX permission test on Windows (#15)

## [0.5.2] - 2026-08-12

### Bug Fixes

- **pkg/tasks:** Route conflict warnings to stderr

## [0.5.1] - 2026-08-12

### Bug Fixes

- **transaction:** Commit must finalize transactions with no backups
- **ci:** Update golangci-lint version to v2.11.4

### Features

- **security:** Implement central fileutil.SafePath helpers and resolve G304/G110 warnings
- **fileutil:** Introduce reusable ExtractTarGz helper and simplify extractTarGz in pull.go

### Refactor

- **tests:** Modularize TestMonorepoGovernance into step helpers
- **core:** Modularize RunApply and executeAndFinalize into pipeline step helpers
- **cmd:** Modularize runNew by extracting bootstrapProjectDir helper
- **tasks:** Introduce WriteManagedFile helper and simplify CreateTemplateTask and LicenseTask
- **tasks:** Extract appendFileContent and trackTaskFile helpers in pkg/tasks
- **cmd:** Replace custom splitOnce with standard strings.SplitN in parseSetFlags
- **templates:** Introduce tasks.BuildTemplateTasks factory to deduplicate handler boilerplate

### Testing

- **testutil:** Consolidate createTarGz test helper into internal/testutil

## [0.5.0] - 2026-08-09

### Bug Fixes

- **security:** Enforce target containment in AppendFileTask and CreateDirTask
- **security:** Correct boundary check in CreateTemplateTask path validation
- **pkg/tasks:** Locate InsertXMLTask targets structurally via encoding/xml token stream
- **pkg/tasks:** Reject undeclared namespace prefixes in ElementPath matching

### Documentation

- Add mermaid diagrams for execution flow and transaction logic

### Features

- Enhance and document native monorepo support

### Styling

- **pkg/tasks:** Gofmt alignment for splice boundary test table

### Testing

- **transaction:** Add priority-band integration tests for execution engine and rollback
- **security:** Add XML parsing boundary, path traversal, and schema fuzz tests

## [0.4.1] - 2026-06-09

### Features

- **schema:** Template input validation with pattern/enum, wire templates to metadata, extend to lang packs

## [0.4.0] - 2026-06-09

### Bug Fixes

- **new:** Correct os.Chdir side-effect and cleanup logic
- **task:** Correct file overwrite logic and implement --force
- **ui:** Unify logging in 'new' command
- **new:** Correct unexported field access idiomatically
- **core:** Make 'apply' robust on empty repositories
- **core:** Fix 'smart' template logic by using future manifest
- **lang/go:** Make Go handler idempotent
- **core:** Prevent duplicate manifest entries
- **git:** Prevent commit failure on a clean repo
- **core:** Correct project name resolution in buildPlan
- **core, lang:** Sanitize project names for Go modules
- **lang/svelte:** Run vite scaffolder from parent directory
- **lang/spring:** Run curl and unzip from parent directory
- **lang/spring:** Use robust Spring Initializr URL
- **lang/spring:** Encode project name and sanitize package for Spring Initializr URL
- **spring:** Correctly handle unzip pathing and consistent chmod execution
- **executor:** Corrected spinner animation
- **main.go.tpl:** Added comment to package for proper linting.
- **template:** Adjusted templates to apply after language packs are applied.
- **svelte_linter:** Linter correctly uses ESLint 9 for newer versions of Svelte.
- **new:** New now correctly allows initialization without a lang for project root setup
- **apply/run:** Improved path logic for Manifest and TargetPath:
- **spring.go:** Moved target directory check to correct position and added an Idempotency check.
- **svelte.go:** Added defensive checks and explicit script setup
- **Dockerfile:** Simplified devcontainer Dockerfile to use standard devcontainer base image
- **Makefile:** Makefile is now more robust and correctly handles languages
- **pom_snippet:** Added explicit checkstyle.xml location
- **cmd/apply:** Resolve linting issues in applyCmd
- **cmd/list:** Resolve linting issues in listCmd
- **cmd/new:** Resolve multiple linting issues
- **cmd/root:** Resolve linting issues in rootCmd
- **core/executor:** Resolve mnd and package-comments
- **manifest/io:** Resolve errcheck and revive warnings
- **internal/pkgs:** Add missing package comments to core modules
- **types/priority:** Resolve revive warnings and naming conflicts
- **lang:** Resolve all language handler revive warnings
- **templates:** Resolve all template handler revive warnings
- **git/checks:** Resolve noctx, nilerr, and revive warnings
- **types/priority:** Add comments for constants
- **perms:** Resolve structural var-naming warnings
- **git:** Reset to savepoint tag instead of HEAD during rollback
- **tests:** Refactor git integration tests for linter compliance
- Ci.yml moved to correct directory
- **config:** Remove deprecated keys causing golangci-lint failure
- **config:** Remove version key causing golangci-lint failure
- **test:** Configure Git identity in TestMain to fix commit failure
- Handle errors for git config in TestMain to satisfy errcheck
- **git:** Set local user identity in unit tests to resolve HEAD failure
- Corrected linting issue
- Changed branch name to master to reflect repository structure
- **transaction/util.go:** Moved a linter comment for clarity
- Resolve path traversal errors and isolate integration tests
- **git_test:** Isolate git identity using environment variables
- **.golangci.yml:** Add version field for golangci-lint v2 compatibility
- **pkg/templates/maven_linter:** Integrate plugin into pom.xml
- **pkg/lang,pkg/templates:** Add mutex protection to handler registries
- **internal/ui:** Add mutex protection to spinner goroutine access
- **internal/ui:** Synchronize output to prevent concurrent write corruption
- **internal/core:** Add timeout context to prevent indefinite hanging
- **pkg/tasks:** Optimize memory allocations in insertXMLElement
- **internal/types:** Change priority initialization panic to error return
- **internal/manifest:** Log cleanup errors instead of swallowing them
- **internal/core:** Validate and warn on empty handler results
- **internal/types:** Implement proper deep copy for manifest
- **internal/ui:** Prevent goroutine leaks in spinner with context timeout
- **internal/manifest:** Implement optimistic locking to detect concurrent modifications
- **deps:** Bump Go toolchain to 1.22.11 to address stdlib CVEs
- Remove unused nolint directives
- **test:** Update CreateTemplateTask test for state-aware reconciliation
- Restore nolint:cyclop for executeAndFinalize after state-aware changes
- Update example rust handler to match new GetTasks signature
- Resolve templates from nested registry cache directories

### Build

- Add gitignore for binaries and artifacts

### Continuous Integration

- Add test and build pipeline
- Update golangci-lint to v2.11 for version field compatibility
- Upgrade golangci-lint to v2.11 with proper v2 configuration

### Documentation

- **project:** Add community health files
- **README.md:** Update README to reflect codebase
- Update for InsertXMLTask and Maven Linter fix
- Document --conflict-strategy flag in README

### Features

- Add initial CLI structure with Cobra
- Define core project types
- Implement global flags
- **task:** Implement 'ExecCommand' task primitive
- Implement 'Plan' executor (core logic)
- **core:** Add Git pre-flight safety checks
- **core:** Implement Git snapshot and rollback
- **core:** Implement Git commit on success
- **apply:** Wire core 'apply' command lifecycle
- Add pre-flight check for external binaries
- **task:** Implement 'CreateTemplate' task primitive
- **lang:** Add 'go' language handler
- **apply:** Connect 'apply --lang' to language handlers
- Implement 'scbake.toml' manifest logic
- **apply:** Update 'apply' to be manifest-aware
- **task:** Make templates 'smart' (context-aware)
- **template:** Add 'makefile' synthesis template
- **apply:** Connect 'apply --with' to tooling templates
- **apply:** Implement path-scoped application
- **new:** Implement 'new' command wrapper
- **list:** Implement 'list' subcommands
- **ui:** Add simple, library-free spinners
- **ui:** Refine step-by-step logging
- **lang/svelte:** Add Svelte handler implementation
- **core:** Register Svelte and add pre-flight checks
- **lang/spring:** Add Spring Boot handler implementation
- **core:** Register Spring and add pre-flight checks
- **spring:** Add CreateDirTask and ensure target directory exists before Spring project setup
- **template:** Add 'ci_github' template for GitHub Actions
- **template:** Add 'editorconfig' template
- **template:** Add 'go_linter' template
- **template:** Add 'maven_linter' template
- **template:** Add 'svelte_linter' template
- **template:** Add 'devcontainer' template
- **devcontainer:** Improved devcontainer.json structure for better DX
- **golangci template:** Improved template quality
- Add priority band system and update language/template handlers
- **README:** Added readme file
- **ci:** Finalize CI pipeline and enable manual run
- **transaction:** Add atomic filesystem transaction manager with rollback and tests
- **manifest:** Implement recursive root discovery and atomic save logic
- **tasks:** Wire transaction manager into task execution
- **core:** Replace git dependency with native transaction engine
- **templates:** Add git version control template
- **templates/git:** Enforce deterministic version control lifecycle
- **pkg/tasks:** Add InsertXMLTask for XML file modifications
- **release:** V0.0.1 release with artifact publishing and documentation
- **install.sh:** Add cross-platform installer script
- Add community and compliance governance templates
- Add governance compliance features (v0.1.0)
- Implement state-aware reconciliation and conflict strategies
- Add state-aware reconciliation and conflict strategies (v0.2.0)
- Implement template overrides (BYOT) mechanism
- Add BYOT template overrides mechanism (v0.3.0)
- Add private template registry system
- **private-registry:** Add auth, versioning, subdirectory, and template validation
- Enable remote registry overrides for surgical snippets

### Miscellaneous Tasks

- Remove redundant pathing logic and unify Spring execution environment
- **go.go:** Documentation and code housekeeping
- **lint:** Add golangci-lint configuration
- **License:** Added license text
- **tasks:** Suppress false positive gosec linter errors
- **legal:** Add GPL-3.0-or-later SPDX headers to configurations
- **lang:** Align registry behavior with template registry
- Bump version from v0.0.1 to v0.1.0
- Bump version from v0.1.0 to v0.2.0
- Bump version from v0.2.0 to v0.3.0

### Performance

- **pkg/tasks:** Optimize XML operations and add comprehensive tests

### Refactor

- **lang:** Make handlers path-aware
- **core:** Consolidate manifest save operation
- **core:** Remove unused manifest parameter from buildPlan
- Remove internal comments and deprecated fields
- **cmd/list:** Decouple list command from hardcoded resources
- **core/run:** Reduce complexity and clean up linting issues
- **tasks/create_template:** Reduce complexity and improve pre-checks
- **templates:** Standardize handler instantiation
- Fully replace Git-based atomicity with custom FS transaction manager
- Centralize file markers and permissions in fileutil
- **ui,core,cmd:** Decouple terminal presentation from execution engine

### Testing

- **util:** Add unit tests for module name sanitization
- **types:** Add concurrency-safe priority sequence tests
- **manifest:** Add load and save persistence tests
- **git:** Add integration suite and fix error propagation
- **tasks:** Add unit tests for exec command task
- **tasks:** Add unit tests for directory creation
- **tasks:** Add unit tests for template creation logic and security checks
- Add integration suite and fix pathing in 'new' command
- **lang/go:** Validate task planning and priority safety
- **lang/spring:** Validate initialization sequence and priority bands
- **lang/svelte:** Validate bootstrap flow and priority bands
- **template/ci-github:** Validate workflow path and CI priority band
- **template/devcontainer:** Enforce structural integrity and deterministic sequencing
- **template/editorconfig:** Enforce deterministic config band placement
- **template/go-linter:** Enforce deterministic linter band placement
- **template/svelte-linter:** Enforce strict priority sequencing and structural integrity
- **template/makefile:** Enforce deterministic build system placement
- **maven_linter:** Add integration and edge case tests
- Expand execution path coverage for governance features
- Expand execution path coverage for state-aware reconciliation
- Expand execution path coverage for BYOT template overrides

### Report

- **docs:** Deduplicate package headers into single doc files

