# Contributing to scbake

Hello! Thank you for your interest in contributing.

scbake aims to stay a small, focused, dependable tool. Contributions are welcome as
long as they respect that goal. Please read [docs/SCOPE.md](docs/SCOPE.md) before
proposing a substantial feature.

## Scope

Contributions may include bug fixes, performance improvements, documentation, tests, or
API extensions consistent with the existing design. Significant or breaking changes
should be discussed in an issue first.

What belongs in core, and what belongs in an extension, is defined in
[docs/SCOPE.md](docs/SCOPE.md).

## Ways to contribute

- **Report a bug** using the [bug report form](.github/ISSUE_TEMPLATE/bug_report.yml).
- **Request a feature** using the
  [feature request form](.github/ISSUE_TEMPLATE/feature_request.yml).
- **Ask a question** in [Discussions](https://github.com/Emin-ACIKGOZ/scbake/discussions).
- **Write code or docs** — look for issues labeled `good first issue` and
  `help wanted`.
- **Report a vulnerability** privately — see [SECURITY.md](.github/SECURITY.md). Do not
  open a public issue for security problems.

Triage labels and the review flow are described in [docs/TRIAGE.md](docs/TRIAGE.md).

## AI usage

AI-assisted contributions are welcome but must be disclosed and must be reviewed and
understood by you. See [AI_USAGE_POLICY.md](AI_USAGE_POLICY.md).

## Development setup

Requires **Go 1.26+** (see `go.mod`).

```sh
git clone https://github.com/Emin-ACIKGOZ/scbake.git
cd scbake
go build -o scbake main.go
go test ./...
```

Useful commands:

| Command | Purpose |
| :------ | :------ |
| `go test ./...` | Run the test suite |
| `go test -race ./...` | Run with the race detector |
| `golangci-lint run ./...` | Lint (uses `.golangci.yml`) |
| `go vet ./...` | Vet checks |
| `go fmt ./...` | Format |
| `go generate ./...` | Regenerate the embedded SPDX license data |
| `./scbake list` | Run the built binary |

## Code standards

- Go version must match `go.mod`.
- Code must be formatted with `gofmt`.
- `go vet` must pass.
- `golangci-lint` must pass using the repository's `.golangci.yml`.
- Public APIs must have clear, idiomatic Go doc comments.
- Avoid new dependencies unless strictly necessary (the standard library is preferred).
- Keep changes minimal and focused.

## Tests

- All behavior changes must include tests.
- Existing tests must pass:
  ```sh
  go test ./...
  ```
- New behavior should include edge-case and failure-path coverage.
- A regression test is expected for every bug fix.

## Commits

scbake uses [Conventional Commits](docs/COMMIT_MESSAGES.md) — release versions are
derived from commit messages, so the format matters.

- Each commit must do exactly one logical thing.
- Do not mix refactors, formatting, and behavior changes in a single commit.
- Write commit messages as documented in [docs/COMMIT_MESSAGES.md](docs/COMMIT_MESSAGES.md).
- Disclose AI assistance with an `Assisted-by` trailer (see
  [AI_USAGE_POLICY.md](AI_USAGE_POLICY.md)).

All commits must carry a Developer Certificate of Origin sign-off:

```sh
git commit -s
```

This adds a `Signed-off-by: Name <email>` trailer certifying that you have the right to
submit the contribution under the project license. See [DCO](DCO).

## Pull requests

- Clearly describe the problem and the change.
- Reference the relevant issue (`Closes: #123`) where applicable.
- Branch from `master` and keep the branch up to date.
- Do not include generated files unless required.
- Keep PRs contained; split large changes into smaller PRs.
- Ensure all CI checks pass.

## Review and merge

Every pull request should be reviewed by a maintainer (see
[MAINTAINERS.md](MAINTAINERS.md)). Maintainers may ask for changes before merging, and
generally prefer a clean history (rebase or squash as appropriate). A change is merged
when it is reviewed, tested, and consistent with [docs/SCOPE.md](docs/SCOPE.md).

## Becoming a maintainer

Contributors who make sustained, high-quality contributions and participate
constructively in issues and reviews may be invited to become maintainers. Preference
is given to people who:

1. Have a track record of merged contributions (code and/or docs).
2. Give thoughtful, respectful review feedback.
3. Have the time to participate regularly.

Maintainers are listed in [MAINTAINERS.md](MAINTAINERS.md) and are nominated by existing
maintainers.

## Compatibility

scbake is **pre-1.0**. Until a stable release, minor versions may include breaking
changes to the manifest format, the CLI, or the Go APIs. Changes that break existing
`scbake.toml` files or invocations should be called out in the commit and the changelog.
A formal deprecation policy will be introduced at `1.0`.

## License

scbake is licensed under the [Apache License 2.0](LICENSE). By contributing, you agree
that your contributions are licensed under the same license and that you have certified
this under the [Developer Certificate of Origin](DCO).
