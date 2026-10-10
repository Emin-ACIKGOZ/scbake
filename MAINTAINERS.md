# Maintainers

This file lists scbake's maintainers and the areas they own. It is intentionally small
so that additional maintainers can be added by editing one table (and the matching
[CODEOWNERS](.github/CODEOWNERS) line).

## Current maintainers

| Name | GitHub | Areas | Since |
| :--- | :----- | :---- | :---- |
| Emin Salih Açıkgöz | [@Emin-ACIKGOZ](https://github.com/Emin-ACIKGOZ) | all | 2025 |

## Areas

| Area | Path | Owner |
| :--- | :--- | :---- |
| CLI | `cmd/` | @Emin-ACIKGOZ |
| Language packs | `pkg/lang/` | @Emin-ACIKGOZ |
| Tooling templates | `pkg/templates/` | @Emin-ACIKGOZ |
| Tasks | `pkg/tasks/` | @Emin-ACIKGOZ |
| Core engine | `internal/core/` | @Emin-ACIKGOZ |
| Transactions | `internal/filesystem/` | @Emin-ACIKGOZ |
| Manifest | `internal/manifest/` | @Emin-ACIKGOZ |
| Schema | `internal/schema/` | @Emin-ACIKGOZ |
| Registry | `internal/templateregistry/` | @Emin-ACIKGOZ |
| Docs | `docs/`, `*.md` | @Emin-ACIKGOZ |

## Responsibilities

Maintainers are expected to:

- Review and merge pull requests.
- Triage issues according to [docs/TRIAGE.md](docs/TRIAGE.md).
- Keep the core small and in line with [docs/SCOPE.md](docs/SCOPE.md).
- Enforce the [Code of Conduct](.github/CODE_OF_CONDUCT.md).
- Cut releases according to [RELEASING.md](.github/RELEASING.md).

## Review policy

Every change should be reviewed by a maintainer. While scbake has a single maintainer,
that maintainer may merge their own changes; the standing goal is that, as soon as a
second maintainer is added, no change is merged without review by someone other than
its author.

## Becoming a maintainer

See the "Becoming a maintainer" section in [CONTRIBUTING.md](CONTRIBUTING.md).
Candidates are nominated by existing maintainers based on sustained, high-quality
contributions and helpful participation in issues and reviews.
