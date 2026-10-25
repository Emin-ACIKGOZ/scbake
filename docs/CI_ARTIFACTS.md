# CI build artifacts and provenance

The [CI workflow](../.github/workflows/ci.yml) uploads a coverage report and
one binary per target. This page explains what those artifacts are and how to
verify that CI produced them.

Release assets are different: they are built by GoReleaser and have their own
SBOM, cosign signature, and provenance. See
[INSTALLING.md](INSTALLING.md#verifying-a-release) for release verification.

## What CI uploads

| Job | Artifact | Contents |
| :-- | :------- | :------- |
| `Coverage gate` | `coverage` | `coverage.out`, the Go coverage profile |
| `Build (<os>/<arch>)` | `scbake-<os>-<arch>` | `scbake-<os>-<arch>` (`scbake-windows-<arch>.exe` on Windows) |

The build matrix is Linux, macOS, and Windows on amd64 and arm64: six
artifacts. The binaries are test artifacts, not release binaries.

## When provenance is generated

The workflow attests both artifact kinds with
[GitHub Artifact Attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations)
and `actions/attest-build-provenance`, before uploading them. An attestation is
created for:

- pushes to `master`;
- manual `workflow_dispatch` runs;
- pull requests whose branch is in this repository.

Only pushes to `master` and `workflow_dispatch` runs on `master` satisfy the
`refs/heads/master` pin described below; a dispatch from another branch records
that branch's ref.

Fork and Dependabot pull requests skip attestation: GitHub gives those runs a
read-only token and no OIDC token, so they cannot create attestations. Their
tests and uploads still run. A skipped attestation does not fail the job.

## Verify an artifact from master

Requirements: a current `gh` CLI (attestation support was added in 2.49) and
network access.

```sh
REPO=Emin-ACIKGOZ/scbake
RUN=$(gh run list --repo "$REPO" --workflow ci.yml --branch master \
  --status completed --limit 1 --json databaseId --jq '.[0].databaseId')

# Coverage
gh run download "$RUN" --repo "$REPO" --name coverage
gh attestation verify coverage.out --repo "$REPO" \
  --signer-workflow "$REPO/.github/workflows/ci.yml" \
  --source-ref refs/heads/master

# One binary; repeat with another artifact name for the other targets.
gh run download "$RUN" --repo "$REPO" --name scbake-linux-amd64
gh attestation verify scbake-linux-amd64 --repo "$REPO" \
  --signer-workflow "$REPO/.github/workflows/ci.yml" \
  --source-ref refs/heads/master
```

`--signer-workflow` and `--source-ref` are what make this meaningful: without
them, any attestation associated with the repository would pass. To pin the
exact source commit as well, add
`--source-digest "$(git rev-parse origin/master)"` to `gh attestation verify`.

## Verify a pull request artifact

A PR artifact records the PR merge ref, not `master`:

```sh
gh run download <run-id> --repo Emin-ACIKGOZ/scbake --name scbake-linux-amd64
gh attestation verify scbake-linux-amd64 --repo Emin-ACIKGOZ/scbake \
  --signer-workflow Emin-ACIKGOZ/scbake/.github/workflows/ci.yml \
  --source-ref refs/pull/<number>/merge
```

Treat PR artifacts as unreviewed. They are useful for testing a proposed
change, not for deciding what to run in production.

## What the attestation proves

It binds the artifact digest to a GitHub Actions run of this repository's
`.github/workflows/ci.yml` at the recorded ref and commit, signed with a
short-lived Sigstore certificate from GitHub's OIDC issuer. For this public
repository the attestation is also recorded in a public transparency log.

It does not prove that the change was reviewed, that tests passed, that the
binary is free of vulnerabilities, or that a CI binary matches a release
binary. Releases are rebuilt by GoReleaser and attested separately.

## Attestation lifetime

Attestations are stored by GitHub, separately from the artifact archive.
Deleting or expiring an artifact (the default retention applies) does not
delete its attestation, but a deleted artifact can no longer be downloaded and
verified.
