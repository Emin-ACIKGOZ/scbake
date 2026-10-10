# AI Usage Policy

scbake welcomes contributions that use AI tools, provided the work is disclosed and
the contributor stands behind it. This policy exists to reduce maintainer fatigue and
to keep reviews a discussion between people.

This policy applies to issues, pull requests, discussions, and code submitted to this
repository.

## Disclosure

Use of AI tools (for example ChatGPT, Claude, Copilot, or similar) **MUST** be
disclosed:

- In commits, add an `Assisted-by: <tool name>` and/or `Co-authored-by:` trailer.
- In issues, pull requests, and discussions, state plainly that AI was used.

## Responsibility

- You are **fully responsible** for anything you submit, whether or not AI produced it.
- AI-generated code **MUST** be understood, reviewed, and tested by you before you
  submit it. Do not submit output you cannot explain.
- Conversations should remain human. Pull request descriptions, review replies, and
  issue discussions **SHOULD** be written in your own words, not pasted from a model.

## What is not allowed

- Autonomous agents opening, commenting on, or closing issues and pull requests on
  behalf of a human without that human's review.
- Unreviewed, low-quality, or bulk AI-generated changes.
- Using AI to misrepresent the origin or authorship of a contribution.

## Enforcement

Maintainers **MAY** close any issue, pull request, or discussion that does not follow
this policy, and **MAY** ask for changes before review continues. Repeated violations
may result in restrictions under the [Code of Conduct](.github/CODE_OF_CONDUCT.md).

## References

- [Conventional Commits](docs/COMMIT_MESSAGES.md) — commit trailer format used for
  disclosure.
- This policy is modeled on the AI policies of peer projects in the Go CLI and
  scaffolding space (GoReleaser, Task, Copier).
