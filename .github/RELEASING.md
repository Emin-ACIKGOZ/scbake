# Release Process

This document describes how to release a new version of scbake.

## Versioning

scbake follows [Semantic Versioning](https://semver.org/):

- **MAJOR.MINOR.PATCH** (e.g., 1.2.3)
- MAJOR: Breaking changes (manifest format, core APIs)
- MINOR: New features (language packs, templates, backward compatible)
- PATCH: Bug fixes, performance improvements

## Pre-Release Checklist

Before cutting a release:

- [ ] All tests pass: `go test ./... -race`
- [ ] No linting issues: `golangci-lint run ./...`
- [ ] No vet warnings: `go vet ./...`
- [ ] Code formatted: `go fmt ./...`
- [ ] Coverage maintained: >75% across all packages
- [ ] No uncommitted changes: `git status`
- [ ] On main branch: `git branch`

## Release Steps

### 1. Update Version in Code

Edit `cmd/root.go`:

```go
var version = "v0.1.0"  // Update from current version
```

### 2. Update CHANGELOG

Edit `CHANGELOG.md`:

```markdown
## [0.1.0] - 2025-04-20

### Added
- New feature 1
- New feature 2

### Fixed
- Bug fix 1
- Bug fix 2

### Changed
- Breaking change (if applicable)

### Security
- Security fix (if applicable)
```

Move "Unreleased" section to versioned section with date.

### 3. Commit Changes

```bash
git add cmd/root.go CHANGELOG.md
git commit -m "chore(release): bump version to v0.1.0"
```

### 4. Tag Release

```bash
git tag -a v0.1.0 -m "Release v0.1.0"
```

### 5. Push to GitHub

```bash
git push origin main
git push origin v0.1.0
```

### 6. Create GitHub Release

On GitHub:

1. Go to Releases page
2. Click "Draft a new release"
3. Select tag: `v0.1.0`
4. Title: `v0.1.0`
5. Description: Copy from CHANGELOG.md

Or via gh CLI:

```bash
gh release create v0.1.0 --title "v0.1.0" --notes-file <(sed -n '/^## \[0.1.0\]/,/^## \[/p' CHANGELOG.md | head -n -1)
```

## Version Numbering Examples

### v0.0.1 → v0.0.2 (Patch: bug fix)
- Fixed symlink traversal in path validation
- Fixed race condition in manifest loading
- Performance optimization for XML insertion

### v0.0.1 → v0.1.0 (Minor: new features)
- Added config-driven extension discovery
- Added new `python_linter` template
- Refactored built-in handlers to unified system

### v0.1.0 → v1.0.0 (Major: breaking changes)
- Changed manifest format from TOML to YAML
- Renamed `scbake apply` to `scbake extend`
- Removed deprecated `--with` flag

## Hotfix Releases

For urgent bug fixes:

1. Create branch from tag:
   ```bash
   git checkout -b hotfix/v0.0.2 v0.0.1
   ```

2. Fix the bug, commit, push

3. Tag and release: `v0.0.2`

4. Merge back to main:
   ```bash
   git checkout main
   git merge hotfix/v0.0.2
   git push origin main
   ```

## Post-Release

After release:

1. **Announce**: Post on r/golang, HN, Twitter (optional)
2. **Monitor**: Watch for issues in the first week
3. **Document**: Update installation docs if needed
4. **Plan**: Outline next minor/major version features

## GitHub Actions (Future)

When automated releases are needed:

```yaml
# .github/workflows/release.yml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - run: go test ./... -race
      - run: go build -o scbake main.go
      - uses: softprops/action-gh-release@v1
        with:
          files: scbake
```

## Continuous Deployment (Future)

When infrastructure permits:

- Build binaries for multiple platforms (Linux, macOS, Windows)
- Auto-generate `scbake` binary on GitHub releases
- Publish to Homebrew formula
- Update documentation site

For now, releases are source-only.

## Version Constraints

Maintain backward compatibility:

- **v0.0.1 projects** must work with v0.1.0, v0.2.0, etc.
- `scbake.toml` format changes require migration tooling
- Command interface changes require deprecation warnings
- Handler API changes (rare) should follow Go compatibility rules

## Questions?

- When should I release? When a meaningful feature is complete + tests pass
- Should I do every commit? No, batch related changes into minor/patch versions
- Can I skip semver? No, users depend on version numbers for compatibility
