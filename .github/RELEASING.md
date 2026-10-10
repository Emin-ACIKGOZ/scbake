# Release Process

scbake releases are **automated**. You do not hand-write versions or tags. Merging a
release-worthy commit to `master` triggers the [Release workflow](workflows/release.yml),
which computes the next version, updates `CHANGELOG.md`, creates the tag, and publishes
binaries.

## Version model

Versions are derived from [Conventional Commits](docs/COMMIT_MESSAGES.md) via
[`svu`](https://github.com/caarlos0/svu):

| Commit | Release |
| :----- | :------ |
| `fix:`, `perf:` | patch |
| `feat:` | minor |
| `!` / `BREAKING CHANGE:` | **minor** while `0.x` (see `.svu.yml` `v0: true`) |
| `docs:`, `chore:`, `ci:`, `test:`, `style:`, `refactor:`, `build:` | none |

The **git tag is the single source of truth**. The binary version is injected at build
time (`-X scbake/cmd.version={{ .Tag }}`); the code no longer hard-codes a version. At
`1.0`, revisit `.svu.yml` so breaking changes bump the major version.

## What triggers a release

- A push or merge to `master` that contains at least one release-worthy commit since the
  last tag.
- Manually, via **Actions → Release → Run workflow** (`workflow_dispatch`).

If there are no release-worthy commits, the workflow exits without releasing.

## Pipeline

1. `svu next` computes the next version from the git history.
2. [`git-cliff`](https://git-cliff.org) regenerates `CHANGELOG.md`.
3. The workflow commits the changelog (`chore(release): vX.Y.Z`), creates an annotated
   tag `vX.Y.Z`, and pushes both.
4. [GoReleaser](https://goreleaser.com) cross-builds `linux`/`darwin`/`windows` ×
   `amd64`/`arm64`, produces the assets below, and creates the GitHub Release.

## Asset contract

These names are **required by `install.sh`** and the README install one-liner. Do not
change them without updating `install.sh` and `README.md`:

- Binaries: `scbake-<os>-<arch>` (`scbake-windows-<arch>.exe` on Windows).
- Checksums: a file named exactly `SHA256SUMS`.

## Guardrails

- `master` is protected: changes land through a pull request; commit messages are
  checked by `commitlint`; the PR title is checked (squash merges make the title the
  released commit).
- The release job **pushes a commit and a tag** to the protected `master` branch. It
  authenticates with a dedicated **deploy key** (`RELEASE_DEPLOY_KEY` secret) that is a
  bypass actor on the `master protection` ruleset. `GITHUB_TOKEN` cannot push to a
  protected branch, and GitHub Actions cannot be a ruleset bypass actor on a
  user-owned repository. Human contributors must use pull requests.

## Repository setup (one-time, already applied)

This repository relies on the following GitHub configuration:

- **Ruleset** `master protection` (branch target `refs/heads/master`): requires a pull
  request and the `Lint`, `Coverage gate`, `Commit messages`, and `PR title` status
  checks; blocks force-pushes and deletions. A **deploy key** is the sole bypass actor.
- **Deploy key** `scbake release bot` (write access) + the `RELEASE_DEPLOY_KEY` secret.
- **Private vulnerability reporting** enabled; **Discussions** enabled.
- **Labels** for triage (see [docs/TRIAGE.md](../docs/TRIAGE.md)).

## Correcting a bad release

If the wrong version was tagged or released:

1. Delete the release and tag:
   ```sh
   gh release delete vX.Y.Z --yes
   git push origin --delete vX.Y.Z
   ```
2. Fix the cause. If the tag was not yet consumed by users, you may amend history;
   otherwise add a corrective commit (`fix:` or `revert:`).
3. Re-run the Release workflow (or push a new release-worthy commit).

Never rewrite or delete a tag that downstream users may already depend on. Prefer a
follow-up patch release.

## Pre-merge checklist (release-triggering PRs)

- [ ] CI is green (lint, cross-platform tests, coverage).
- [ ] Commit messages and the PR title conform to Conventional Commits.
- [ ] If the manifest or CLI changed and breaks users, the breaking change is called out.

## Permissions and secrets

- The workflow uses the default `GITHUB_TOKEN` with `contents: write`.
- No external secrets or tokens are required.

## Related

- [docs/COMMIT_MESSAGES.md](../docs/COMMIT_MESSAGES.md) — commit conventions.
- [docs/SCOPE.md](../docs/SCOPE.md) — what belongs in core.
- [RELEASING.md](RELEASING.md) — this document.
