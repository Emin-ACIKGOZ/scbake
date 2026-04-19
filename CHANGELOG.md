# Changelog

All notable changes to scbake are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned for v0.1.0
- Config-driven extension discovery (`scbake.ext.toml`)
- Built-in handler refactoring (unified extension system)
- Architecture documentation improvements
- Installation via Homebrew
- GitHub releases with multi-platform binaries

### Planned for v0.2.0+
- Subprocess-based plugin system (if demand exists)
- Command allowlisting for security
- Audit logging
- Plugin registry
- Hot-reload capabilities (unlikely)

---

## [0.0.1] - 2025-04-19

**The first release of scbake: A manifest-driven, atomic project scaffolder.**

### Added

#### Core Features
- ✅ **Atomic operations** - All changes applied transactionally with LIFO rollback
- ✅ **Manifest tracking** - `scbake.toml` as source of truth for project state
- ✅ **Priority-based execution** - Tasks execute in defined bands (directory creation → language setup → config → linters → build → VCS)
- ✅ **Dry-run mode** - Preview changes before applying with `--dry-run`
- ✅ **Transaction safety** - Automatic rollback on failure, filesystem restored to original state

#### Commands
- `scbake new <name>` - Create new project with language pack and templates
- `scbake apply` - Apply templates to existing projects
- `scbake list [langs|templates|projects]` - View available resources

#### Language Packs (3)
- **Go** - Creates project structure, runs `go mod init`, `go mod tidy`
- **Spring** - Bootstrap Java projects from `start.spring.io`
- **Svelte** - Initialize Vite+Svelte frontend with npm

#### Templates (8)
- **git** - Initialize Git repo with first commit
- **editorconfig** - Standard `.editorconfig` for IDE consistency
- **makefile** - Universal `Makefile` with build/test/lint targets
- **ci_github** - GitHub Actions workflows (language-aware)
- **go_linter** - golangci-lint configuration for Go projects
- **maven_linter** - Checkstyle + Maven plugin integration
- **svelte_linter** - ESLint 9 for Svelte projects
- **devcontainer** - VS Code dev container with auto-detected toolchains

#### Task Types (4)
- **CreateTemplateTask** - Create files from embedded templates with variable substitution
- **ExecCommandTask** - Run shell commands in project context
- **CreateDirectoryTask** - Create directories with full path handling
- **InsertXMLTask** - Insert XML fragments into existing files (e.g., Maven pom.xml)

#### Code Quality
- Zero external dependencies (standard library only)
- >75% test coverage across all packages
- Comprehensive integration tests (full end-to-end workflows)
- Fuzz tests for XML operations
- Performance benchmarks (10 benchmarks, optimized)
- Race detection enabled on all tests
- Zero linting issues (golangci-lint passing)

#### Documentation
- `README.md` - Feature overview and usage examples
- `CONTRIBUTING.md` - Contribution guidelines
- `SECURITY.md` - Security policy and threat model
- `docs/QUICK_START.md` - 5-minute getting started guide
- `docs/EXTENDING.md` - Complete guide to adding custom handlers
- `docs/ARCHITECTURE.md` - Architecture overview for contributors

#### Security
- Path traversal prevention in all file operations
- Optimistic locking for manifest conflict detection (concurrent modifications)
- Symlink validation in transaction system
- Secure temporary file handling (0600 permissions)
- Atomic manifest updates (write to temp file, then rename)
- No arbitrary code execution (all handlers compiled-in)

### Known Limitations

- **No dynamic plugins** - Handlers must be compiled in. Fork & modify for custom extensions.
- **Single project per manifest** - One `scbake.toml` per directory (monorepo support planned)
- **Limited monitoring** - No audit log of executed commands (planned for v0.2.0)
- **No symlink hardening** - Symlink validation present but not comprehensive (defer to v0.1.0)
- **Windows support untested** - Developed on Linux, should work on Windows but not verified
- **No version pinning** - Always pulls latest (e.g., Spring Boot latest, npm packages latest)

### Performance
- Sub-10ms handler discovery (interface-based dispatch)
- <50ms typical project creation (dominated by I/O and command execution)
- ~500KB memory overhead for 50 templates
- Single-pass transaction processing with O(n) rollback

### Testing Summary
- **19 test packages** with >75% coverage
- **45+ individual tests** including unit, integration, fuzz, and benchmarks
- **0 race condition issues** (all tests pass with `-race` flag)
- **10 performance benchmarks** showing optimized XML operations (5.4x improvement)
- **Edge cases covered**: missing files, rollback on error, concurrent access, malformed XML

### Breaking Changes
None - This is the first release.

---

## Installation

### From Source

```bash
git clone https://github.com/Emin-ACIKGOZ/scbake.git
cd scbake
go build -o scbake main.go
./scbake --version
```

**Requirements**: Go 1.21+

---

## Next Steps

1. **Read the Quick Start**: See `docs/QUICK_START.md` for a 5-minute walkthrough
2. **Extend scbake**: Add custom handlers - see `docs/EXTENDING.md`
3. **Contribute**: Found a bug or have an idea? Open an issue on GitHub
4. **Share feedback**: Help us improve! Your use cases drive the roadmap.

---

## Versioning Policy

scbake follows [Semantic Versioning](https://semver.org/):

- **MAJOR version** (X.0.0): Breaking changes to the manifest format or core APIs
- **MINOR version** (0.X.0): New features (language packs, templates) and minor improvements
- **PATCH version** (0.0.X): Bug fixes and performance improvements

**Stability guarantees**:
- v0.0.1 is stable for production use (atomic operations, comprehensive tests)
- v0.1.0+ will maintain backward compatibility with v0.0.1 projects
- Breaking changes will be announced with deprecation warnings in advance

---

## Contributors

- **Emin Salih Açıkgöz** (@Emin-ACIKGOZ) - Creator, core architecture, all language packs & templates

---

## License

scbake is dual-licensed under MIT and GPL-3.0-or-later. See LICENSE file for details.
