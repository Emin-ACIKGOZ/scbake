# Issue Triage

This document describes how scbake labels and triages issues. It is written for
maintainers and is intentionally lightweight: scbake is a small project, so triage is
best-effort, not a service-level guarantee.

## Labels

### Status

| Label | Meaning |
| :---- | :------ |
| `needs-triage` | New; not yet reviewed by a maintainer. Applied automatically by the issue forms. |
| `needs-repro` | Confirmed plausible, but a minimal reproduction is still missing. |
| `needs-info` | Waiting on the reporter to answer a question. |
| `confirmed` | Reproduced or clearly understood; ready to be worked on. |
| `stale` | No activity for a while; will be closed if it stays inactive. |
| `duplicate` | Already tracked elsewhere (link to the original). |
| `wontfix` | Intentionally not planned; explain why. |

### Type

| Label | Meaning |
| :---- | :------ |
| `bug` | Something is broken. |
| `enhancement` | A new feature or improvement. |
| `documentation` | Docs-only change or docs problem. |
| `security` | Security-relevant (usually handled privately; see below). |

### Area

`area:cli`, `area:lang`, `area:templates`, `area:manifest`, `area:transaction`,
`area:registry`, `area:schema`.

### Contribution

| Label | Meaning |
| :---- | :------ |
| `good first issue` | Small, well-scoped, newcomer-friendly. |
| `help wanted` | Maintainers would welcome an outside contribution. |
| `blocked` | Waiting on something external. |

## Triage flow

1. **Intake.** A new issue arrives with `needs-triage` from the issue form. Verify the
   form is complete and remove duplicates (`duplicate`).
2. **Classify.** Add exactly one type label and one `area:*` label. If the issue is a
   feature that falls outside [SCOPE.md](SCOPE.md), say so and close with `wontfix`
   (or redirect to an extension/registry).
3. **Reproduce.** For bugs, try the reported command. If you cannot reproduce, add
   `needs-repro` and ask for the exact command, `--dry-run` output, and `scbake.toml`.
   If the report is unclear, add `needs-info`.
4. **Confirm.** Once understood, replace `needs-triage` with `confirmed`. If it is a
   good entry point, add `good first issue`.
5. **Track.** Reference the issue from the fixing commit (`Closes: #123` in a footer)
   so it closes automatically.

## Security issues

Public issues are the wrong channel for vulnerabilities. The issue chooser routes
reporters to **private vulnerability reporting** via GitHub Security Advisories; see
[SECURITY.md](../.github/SECURITY.md). If a sensitive report arrives publicly, remove
it from public view and continue privately.

## Questions

"How do I…" questions belong in **GitHub Discussions**, not the issue tracker. The
issue chooser links there. Close questions opened as issues and redirect.

## Stale policy

Issues with `needs-info` or `needs-repro` may be closed after ~30 days of no response,
with a comment explaining that they can be reopened with the requested information.

## Related

- [SCOPE.md](SCOPE.md) — what belongs in core.
- [COMMIT_MESSAGES.md](COMMIT_MESSAGES.md) — referencing issues from commits.
- [../.github/ISSUE_TEMPLATE/](../.github/ISSUE_TEMPLATE/) — the forms that feed triage.
