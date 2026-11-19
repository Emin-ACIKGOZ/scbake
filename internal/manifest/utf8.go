// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"fmt"
	"unicode/utf8"

	"scbake/internal/types"
)

// validateManifestUTF8 enforces the invariant that every string persisted in
// scbake.toml is valid UTF-8. TOML cannot encode a raw invalid byte, so writing
// one produces a file that the next Load cannot decode. Plan-time validation
// catches the known CLI inputs; this Save-time guard covers every writer.
func validateManifestUTF8(m *types.Manifest) error {
	if m == nil {
		return nil
	}
	if err := checkUTF8("scbake_version", m.SbakeVersion); err != nil {
		return err
	}
	for i, p := range m.Projects {
		if err := validateProjectUTF8(i, p); err != nil {
			return err
		}
	}
	for i, t := range m.Templates {
		if err := validateTemplateUTF8(i, t); err != nil {
			return err
		}
	}
	if err := validateStringMapUTF8("metadata", m.Metadata); err != nil {
		return err
	}
	if err := validateStringMapUTF8("managed_files", m.ManagedFiles); err != nil {
		return err
	}
	if err := validateStringMapUTF8("accepted_files", m.AcceptedFiles); err != nil {
		return err
	}
	return validateStringSliceUTF8("unmanaged_files", m.UnmanagedFiles)
}

func checkUTF8(field, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", field)
	}
	return nil
}

func validateProjectUTF8(i int, p types.Project) error {
	for _, f := range []struct {
		name  string
		value string
	}{
		{"name", p.Name},
		{"path", p.Path},
		{"language", p.Language},
	} {
		if err := checkUTF8(fmt.Sprintf("projects[%d].%s", i, f.name), f.value); err != nil {
			return err
		}
	}
	for j, t := range p.Templates {
		if err := checkUTF8(fmt.Sprintf("projects[%d].templates[%d]", i, j), t); err != nil {
			return err
		}
	}
	return nil
}

func validateTemplateUTF8(i int, t types.Template) error {
	if err := checkUTF8(fmt.Sprintf("templates[%d].name", i), t.Name); err != nil {
		return err
	}
	return checkUTF8(fmt.Sprintf("templates[%d].path", i), t.Path)
}

func validateStringMapUTF8(name string, values map[string]string) error {
	for k, v := range values {
		if err := checkUTF8(fmt.Sprintf("%s key %q", name, k), k); err != nil {
			return err
		}
		if err := checkUTF8(fmt.Sprintf("%s %q", name, k), v); err != nil {
			return err
		}
	}
	return nil
}

func validateStringSliceUTF8(name string, values []string) error {
	for i, value := range values {
		if err := checkUTF8(fmt.Sprintf("%s[%d]", name, i), value); err != nil {
			return err
		}
	}
	return nil
}
