// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package types

// Manifest is the root structure of the scbake.toml file.
// It's the "source of truth" for the project.
type Manifest struct {
	SbakeVersion string     `toml:"scbake_version"`
	Projects     []Project  `toml:"projects"`
	Templates    []Template `toml:"templates"`
}

// Project represents a distinct code unit, like a Go backend or a React frontend.
type Project struct {
	Name      string   `toml:"name"`
	Path      string   `toml:"path"`
	Language  string   `toml:"language"`
	Templates []string `toml:"templates"` // List of template names applied
}

// Template represents a root-level tooling template applied to the repo.
type Template struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}
