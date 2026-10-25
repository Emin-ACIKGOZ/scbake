# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, do **not** open a public issue or pull request.

Instead, report it **privately** by contacting the project maintainer with:
- A clear description of the issue
- Steps to reproduce, if applicable
- Potential impact and affected versions

You may use encrypted communication if preferred.

## Scope

This policy applies to:
- Public APIs and exported behavior
- Validation logic, safety guarantees, and concurrency controls
- Middleware, adapters, and reflection helpers

Issues caused by misuse outside documented guarantees are generally out of scope.

## Build provenance

CI artifacts from trusted runs (pushes to `master` and manual runs on `master`)
and releases are built by GitHub Actions and carry verifiable provenance. Fork
and Dependabot pull request artifacts are intentionally un-attested. See
[CI_ARTIFACTS.md](../docs/CI_ARTIFACTS.md) for CI verification and
[INSTALLING.md](../docs/INSTALLING.md#verifying-a-release) for release
signatures and SBOMs.

## Disclosure Process

Reported vulnerabilities will be reviewed and triaged as soon as possible.  
If confirmed, a fix will be developed and released. Public disclosure may occur after a fix is available.

## Supported Versions

Only the latest released version is supported with security updates unless stated otherwise.

## Responsible Disclosure

Please allow reasonable time for investigation and remediation before public disclosure.
