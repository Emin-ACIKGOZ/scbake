// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Package version records the pinned SPDX License List version.
//
// It deliberately imports nothing and embeds no data, so the generator in
// gen can use it without importing the spdx package that it regenerates. That
// keeps version.go the single source of truth for the embedded version.
package version

// Version is the SPDX License List version embedded in the spdx package.
const Version = "3.29.0"

// Tag is the license-list-data repository tag that provides Version.
const Tag = "v" + Version
