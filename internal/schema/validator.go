// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package schema

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ValidationError describes a single input validation failure.
type ValidationError struct {
	VariableName string
	Message      string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("variable %q: %s", e.VariableName, e.Message)
}

// ValidationResult holds the outcome of validating inputs against a schema.
type ValidationResult struct {
	Errors   []ValidationError
	Warnings []string
}

// HasErrors returns true when at least one validation error exists.
func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// Error returns a human-readable summary of all validation errors.
func (r *ValidationResult) Error() string {
	if !r.HasErrors() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "schema validation failed with %d error(s):\n", len(r.Errors))
	for _, e := range r.Errors {
		b.WriteString("  - " + e.Error() + "\n")
	}
	return b.String()
}

// Validate checks the provided metadata against the schema.
// It returns missing required variables, type mismatches, and
// populates defaults for any optional variables that are absent.
//
//nolint:cyclop,gocognit // Each case is a simple field check; combined they exceed the threshold.
func Validate(s *Schema, metadata map[string]string) *ValidationResult {
	res := &ValidationResult{}

	// Sort variable names for deterministic output
	varNames := make([]string, 0, len(s.Variables))
	for name := range s.Variables {
		varNames = append(varNames, name)
	}
	sort.Strings(varNames)

	for _, name := range varNames {
		def := s.Variables[name]
		val, exists := metadata[name]

		if !exists || val == "" {
			if def.Required {
				res.Errors = append(res.Errors, ValidationError{
					VariableName: name,
					Message:      fmt.Sprintf("required variable %q is missing", name),
				})
			} else if def.Default != nil {
				if metadata != nil {
					metadata[name] = *def.Default
				}
			}
			continue
		}

		// Type validation (only for number and boolean where we can check)
		switch def.Type {
		case TypeNumber:
			if !isNumber(val) {
				res.Errors = append(res.Errors, ValidationError{
					VariableName: name,
					Message:      fmt.Sprintf("expected a number, got %q", val),
				})
				continue
			}
		case TypeBool:
			if !isBool(val) {
				res.Errors = append(res.Errors, ValidationError{
					VariableName: name,
					Message:      fmt.Sprintf("expected true or false, got %q", val),
				})
				continue
			}
		}

		// Pattern (regex) validation
		if def.Pattern != "" {
			matched, err := regexp.MatchString(def.Pattern, val)
			if err != nil {
				res.Errors = append(res.Errors, ValidationError{
					VariableName: name,
					Message:      fmt.Sprintf("invalid pattern %q: %v", def.Pattern, err),
				})
			} else if !matched {
				res.Errors = append(res.Errors, ValidationError{
					VariableName: name,
					Message:      fmt.Sprintf("does not match required pattern %q", def.Pattern),
				})
			}
		}

		// Enum validation
		if len(def.Enum) > 0 {
			found := false
			for _, allowed := range def.Enum {
				if val == allowed {
					found = true
					break
				}
			}
			if !found {
				res.Errors = append(res.Errors, ValidationError{
					VariableName: name,
					Message:      fmt.Sprintf("must be one of %v, got %q", def.Enum, val),
				})
			}
		}
	}

	return res
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isBool(s string) bool {
	return s == "true" || s == "false"
}
