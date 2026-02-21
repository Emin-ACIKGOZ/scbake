# Contributing

Hello! Thank you for showing interest in my project.

This document defines the standard contribution rules used across my Go projects. Individual repositories may add project-specific requirements.


## Scope

Contributions may include bug fixes, performance improvements, documentation updates, tests, or API extensions consistent with existing design. Significant or breaking changes should be discussed in an issue first.


## Code Standards

- Go version must match `go.mod`
- Code must be formatted with `gofmt`
- `go vet` must pass
- `golangci-lint` must pass using the repository’s `.golangci.yml`
- Public APIs must have clear, idiomatic Go doc comments
- Avoid new dependencies unless strictly necessary
- Keep changes minimal and focused


## Tests

- All changes must include appropriate tests
- Existing tests must pass:
  ```sh
  go test ./...
  ```

- New behavior should include edge-case and failure-path coverage


## Commits

- Each commit must do exactly one logical thing
- Do not mix refactors, formatting, and behavior changes in a single commit
- Commit messages should be clear and imperative


## Pull Requests

- Clearly describe the problem and the change
- Reference relevant issues if applicable
- Ensure the branch is up to date with `main`
- Do not include generated files unless required


## API Stability

- Public APIs should remain backward compatible when possible
- Breaking changes require clear justification and documentation
- `internal/` packages are not considered stable


## License

- By contributing, you agree that your contributions are licensed under the same license as the project.

```
Copyright 2025 Emin Salih Açıkgöz

SPDX-License-Identifier: MIT
```
