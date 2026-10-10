# Project Scope

This document defines what belongs in scbake's core and what does not. It exists to
keep the core small, stable, and focused, so that it remains safe to depend on and
cheap to maintain. When a feature is out of scope for core, it is usually in scope as
an **extension** (a language pack, a tooling template, a registry entry, or a fork).

## Purpose

scbake is a **single-binary CLI that applies layered infrastructure templates to a
project atomically**, driven by a manifest (`scbake.toml`) as the source of truth.

Core does one thing well:

1. **Plan** — turn a manifest plus flags into an ordered list of tasks.
2. **Execute** — run those tasks with a LIFO transaction that rolls back on failure.
3. **Extend** — dispatch to handlers (language packs and tooling templates) through a
   small, stable interface.
4. **Distribute** — resolve templates through the override → registry → embedded chain,
   and validate inputs against each template's schema.

## In scope (core)

- The planner, executor, and transaction/rollback system.
- The `Handler` / `Task` interfaces and the built-in task types
  (`CreateTemplateTask`, `AppendFileTask`, `ExecCommandTask`,
  `CreateDirectoryTask`, `InsertXMLTask`).
- Manifest loading, the monorepo root-discovery algorithm, and conflict strategies.
- Template resolution, the registry/cache, and schema validation.
- The CLI surface (`new`, `apply`, `list`, `template`), output, and exit codes.
- Path-safety, atomicity, and the general safety guarantees.

## Out of scope (extensions)

- **Language-specific behavior** beyond what a language pack needs. A new language is a
  new pack under `pkg/lang/`, not a change to core.
- **Opinionated, organization-specific templates.** Ship them as registry entries or
  override directories, not in the default template set.
- **A maintained template library.** scbake ships a reasonable default set; it does not
  curate templates for every ecosystem.
- **A runtime plugin system.** Extensions are compiled in today; see
  [EXTENDING.md](EXTENDING.md). A subprocess/plugin model is out of scope unless there
  is real demand and a design that preserves the atomic-safety guarantees.
- **Network services, daemons, or a hosted registry.** scbake is a local CLI.

## How to decide

A change belongs in core only if **all** of the following hold:

1. It is broadly useful, not specific to one language, vendor, or workflow.
2. It can be expressed through the handler/task interfaces rather than special-cased.
3. It does not weaken the atomic-safety or path-safety guarantees.
4. It does not add a dependency where the standard library or an existing dependency
   would do.

If a proposal fails any of these, prefer an extension. Prefer a new template over a new
core concept; prefer a new task type over a new code path.

## Related

- [EXTENDING.md](EXTENDING.md) — how to add language packs and templates.
- [ARCHITECTURE.md](ARCHITECTURE.md) — how the core is structured.
- [TRIAGE.md](TRIAGE.md) — how scope affects issue triage.
