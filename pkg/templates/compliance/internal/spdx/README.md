# Embedded SPDX license data

This package provides the license texts used by the `compliance` template.
The data is compiled into the `scbake` binary, so `scbake new --with compliance`
and `scbake apply --with compliance` never make a network request to look up a
license.

## Contents

| File | Purpose |
| :--- | :------ |
| `licenses.zip` | One `licenses/<ID>.txt` entry per SPDX license |
| `index.go` | Generated SPDX version and case-insensitive ID index |
| `gen/` | Generator for the two files above |
| `spdx.go` | Runtime lookup API |

The archive is generated from the official
[`spdx/license-list-data`](https://github.com/spdx/license-list-data) repository
and contains **all 740 licenses**, including deprecated IDs. The current data is
the **SPDX License List 3.29.0**.

The ZIP is about 1.9 MB. It is opened lazily on the first license lookup and
only the requested entry is inflated, so commands that do not generate a
license do not open or decompress the archive.

## Regenerating

Run the generator from this directory:

```sh
go generate ./...
```

Or point it at a local checkout of the official data repository:

```sh
go run ./gen -source /path/to/license-list-data
```

Use `-tag vX.Y.Z` to fetch a specific SPDX release. The generator writes
`licenses.zip` and `index.go`; both are committed because they are embedded in
the binary. Do not edit them by hand.

After regenerating, run the package and integration tests:

```sh
go test ./pkg/templates/compliance/...
go test ./tests
```

## Why embedded?

- **Immutable:** a released `scbake` binary always produces the same license
  text for the same inputs.
- **No latency:** no DNS, TLS, or HTTP request is made while generating a
  project.
- **Small attack surface:** license lookup cannot be influenced by a remote
  service or by URL/path handling of user input.
- **Auditable:** the SPDX version and license IDs are visible in the generated
  index, and the archive is covered by the release SBOM and provenance
  attestations.
