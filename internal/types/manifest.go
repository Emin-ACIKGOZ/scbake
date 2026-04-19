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

// DeepCopy creates a complete copy of the manifest including all nested slices.
// This ensures modifications to the copy don't affect the original.
func (m *Manifest) DeepCopy() *Manifest {
	if m == nil {
		return nil
	}

	result := &Manifest{
		SbakeVersion: m.SbakeVersion,
		Projects:     make([]Project, len(m.Projects)),
		Templates:    make([]Template, len(m.Templates)),
	}

	// Deep copy projects
	for i, p := range m.Projects {
		result.Projects[i] = Project{
			Name:      p.Name,
			Path:      p.Path,
			Language:  p.Language,
			Templates: make([]string, len(p.Templates)),
		}
		result.Projects[i].Templates = append([]string{}, p.Templates...)
	}

	// Deep copy templates
	for i, t := range m.Templates {
		result.Templates[i] = Template{
			Name: t.Name,
			Path: t.Path,
		}
	}

	return result
}
