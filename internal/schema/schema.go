// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package schema defines template input schemas and validates manifest
// metadata against them before task execution.
package schema

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// VariableType enumerates the supported input types.
type VariableType string

const (
	// TypeString indicates a string-typed variable.
	TypeString VariableType = "string"
	// TypeNumber indicates a numeric-typed variable.
	TypeNumber VariableType = "number"
	// TypeBool indicates a boolean-typed variable.
	TypeBool VariableType = "boolean"
)

// VariableDef describes a single template input variable.
type VariableDef struct {
	Type        VariableType `json:"type"`
	Required    bool         `json:"required,omitempty"`
	Default     *string      `json:"default,omitempty"`
	Description string       `json:"description,omitempty"`
	Pattern     string       `json:"pattern,omitempty"`
	Enum        []string     `json:"enum,omitempty"`
}

// Schema defines the expected inputs for a template.
type Schema struct {
	Description string                 `json:"description,omitempty"`
	Variables   map[string]VariableDef `json:"variables"`
}

// ReadSchema reads and parses a schema.json from an embedded filesystem.
func ReadSchema(efs embed.FS, path string) (*Schema, error) {
	data, err := efs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return ParseSchemaBytes(data)
}

// ParseSchemaBytes decodes a JSON schema from a byte slice.
func ParseSchemaBytes(data []byte) (*Schema, error) {
	var s Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to decode schema: %w", err)
	}
	if len(s.Variables) == 0 {
		return nil, errors.New("schema must define at least one variable")
	}
	return &s, nil
}

// ParseSchema decodes a JSON schema from r.
func ParseSchema(r io.Reader) (*Schema, error) {
	var s Schema
	if err := json.NewDecoder(r).Decode(&s); err != nil {
		return nil, fmt.Errorf("failed to decode schema: %w", err)
	}
	if len(s.Variables) == 0 {
		return nil, errors.New("schema must define at least one variable")
	}
	return &s, nil
}
