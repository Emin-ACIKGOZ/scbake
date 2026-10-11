// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"scbake/internal/schema"
	"scbake/pkg/lang"
	"scbake/pkg/templates"
)

// ValidateSetKeys rejects --set keys that are not declared by the schema of any
// selected tooling template or language pack. Without this check, arbitrary
// key=value pairs are merged into manifest metadata (see buildTaskContext) and
// reach templates unvalidated, because validateSelectedTemplates only checks
// keys that a selected schema happens to declare.
//
// It returns nil when setVars is empty or every key is declared by at least one
// selected schema. Otherwise it returns an aggregated error naming every
// unknown key along with the set of allowed keys.
func ValidateSetKeys(with []string, langName string, setVars map[string]string) error {
	if len(setVars) == 0 {
		return nil
	}

	declared := make(map[string]struct{})

	// Collect declared variables from the selected tooling templates.
	for _, name := range with {
		efs, path, err := templates.GetSchema(name)
		if err != nil {
			return fmt.Errorf("schema lookup for %q: %w", name, err)
		}
		// A handler that does not implement SchemaProvider returns nil schema:
		// it declares no variables, so skip it.
		if efs == nil {
			continue
		}
		if err := collectSchemaVars(*efs, path, declared); err != nil {
			return fmt.Errorf("failed to read schema for %q: %w", name, err)
		}
	}

	// Collect declared variables from the selected language pack.
	if langName != "" {
		efs, path, err := lang.GetSchema(langName)
		if err != nil {
			return fmt.Errorf("schema lookup for %q: %w", langName, err)
		}
		if efs != nil {
			if err := collectSchemaVars(*efs, path, declared); err != nil {
				return fmt.Errorf("failed to read schema for %q: %w", langName, err)
			}
		}
	}

	unknown := make([]string, 0, len(setVars))
	for key := range setVars {
		if _, ok := declared[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)

	if len(declared) == 0 {
		return fmt.Errorf(
			"unknown --set key(s) %s: no selected template or language declares any variables",
			quoteKeys(unknown),
		)
	}

	allowed := make([]string, 0, len(declared))
	for key := range declared {
		allowed = append(allowed, key)
	}
	sort.Strings(allowed)

	return fmt.Errorf(
		"unknown --set key(s) %s: allowed keys are %s",
		quoteKeys(unknown), strings.Join(allowed, ", "),
	)
}

// collectSchemaVars reads an embedded schema and adds its variable names to the
// declared set.
func collectSchemaVars(efs embed.FS, path string, declared map[string]struct{}) error {
	s, err := schema.ReadSchema(efs, path)
	if err != nil {
		return err
	}
	for name := range s.Variables {
		declared[name] = struct{}{}
	}
	return nil
}

// quoteKeys renders keys as a comma-separated, double-quoted list.
func quoteKeys(keys []string) string {
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = fmt.Sprintf("%q", k)
	}
	return strings.Join(quoted, ", ")
}
