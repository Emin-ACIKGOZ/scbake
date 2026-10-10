# Commit Message Conventions

scbake follows [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/)
for every commit. This is not cosmetic: scbake's release version is **computed from
commit messages**, so the type of a commit determines whether the next release is a
patch, a minor, or a major. A malformed message can produce a wrong tag.

The key words **MUST**, **MUST NOT**, **SHOULD**, and **MAY** in this document are to
be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

## Format

```
<type>[optional scope][!]: <description>

[optional body]

[optional footer(s)]
```

1. `type` — a noun describing the kind of change (see below).
2. `scope` — optional, a noun naming the affected area, in parentheses.
3. `!` — optional marker for a breaking change.
4. `description` — a short summary, immediately after the colon and space.
5. `body` — optional, free-form context on **what** and **why**, one blank line after
   the description.
6. `footers` — optional, one blank line after the body (see
   [git trailer format](https://git-scm.com/docs/git-interpret-trailers)).

## Types

Only the types below are permitted. The release effect assumes scbake's current
pre-1.0 mapping (see [Breaking changes](#breaking-changes-and-the-0x-rule)).

| Type | Use for | Release effect |
| :--- | :------ | :------------- |
| `feat` | A new user-facing capability | minor |
| `fix` | A bug fix | patch |
| `perf` | A performance improvement | patch |
| `refactor` | A change that neither fixes a bug nor adds a feature | none |
| `docs` | Documentation only | none |
| `style` | Formatting / whitespace, no behavior change | none |
| `test` | Adding or correcting tests | none |
| `build` | Build system or dependency changes | none |
| `ci` | Continuous integration changes | none |
| `chore` | Maintenance that fits no other type | none |
| `revert` | Reverting a previous commit | none |

> `feat` and `fix` describe changes a **user** can observe. A change to a build script
> or an internal helper is usually `chore`, `refactor`, or `build` — not `feat`.

## Scope

A scope **MAY** be provided to add context. For scbake, prefer one of the areas below
(the same areas used by [CODEOWNERS](../.github/CODEOWNERS) and issue triage):

`cmd`, `lang`, `templates`, `tasks`, `core`, `transaction`, `manifest`, `registry`,
`schema`, `docs`, `ci`.

```
fix(registry): search nested template subdirectories on pull
feat(templates)!: require schema.json for template variables
docs(commit): add commit message conventions
```

## Description

- **MUST** be written in the imperative mood: "add", not "added" or "adds".
- **SHOULD** be lowercase and **MUST NOT** end with a period.
- **SHOULD** be short enough to read in a `git log --oneline` (aim for ≤ 72
  characters). The hard limit enforced by commitlint is 100.

## Body

A body **MAY** be added one blank line after the description. Use it to explain the
**motivation** and any non-obvious **trade-offs**. Wrap lines at ~72 characters.

## Footers

Footers **MAY** be added one blank line after the body. Common footers:

```
Refs: #123
Closes: #123
Co-authored-by: Name <email@example.com>
Assisted-by: <tool name>
```

`Assisted-by` is scbake's disclosure marker for AI-assisted work; see
[AI_USAGE_POLICY.md](../AI_USAGE_POLICY.md).

## Breaking changes and the 0.x rule

Normally a breaking change maps to a **major** bump. Because scbake is still `0.x`, a
breaking change maps to a **minor** bump (`0.x.0`) to avoid an accidental jump to
`1.0.0`. This is an explicit, temporary project decision that will be revisited at
`1.0`.

Indicate a breaking change **either** with `!` before the colon **or** with a
`BREAKING CHANGE:` footer (both may be used):

```
feat(manifest)!: rename [project] table to [projects]

BREAKING CHANGE: existing scbake.toml files use [project]; run `scbake migrate`.
```

## Examples

```
docs: correct spelling of CHANGELOG
```

```
fix(tasks): restore backed-up files in LIFO order on rollback
```

```
feat(lang): add a Rust language pack
```

```
refactor(core): extract pipeline step helpers from RunApply

Splits manifest loading, planning, and execution into named helpers so
each step can be tested in isolation.
```

## Fixing a bad commit message

- One commit: `git commit --amend` (reword before pushing).
- Several commits: `git rebase -i <base>` and mark them `reword`.
- After a release has been created, do not rewrite published history; instead make a
  corrective commit (e.g. `docs:` or `revert:`) and, if needed, correct the tag as
  described in [RELEASING.md](../.github/RELEASING.md).

## Enforcement

Commit messages are **not** yet enforced mechanically. `commitlint` against
`@commitlint/config-conventional` is planned for CI so that non-conforming messages
fail before merge. Until then, reviewers check messages during review.

## References

- [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/) —
  the specification this document implements (licensed
  [CC BY 3.0](https://creativecommons.org/licenses/by/3.0/)).
- [Angular commit message guidelines](https://github.com/angular/angular/blob/main/CONTRIBUTING.md#commit)
  — the origin of the standard type list.
- [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt) — definitions of MUST/SHOULD/MAY.
- [Semantic Versioning](https://semver.org/spec/v2.0.0.html) — how commit types map to
  version bumps.
