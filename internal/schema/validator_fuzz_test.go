// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build go1.18

package schema

import (
	"testing"
)

// FuzzSchemaVariableValidation tests schema variable validation against arbitrary inputs.
//
//nolint:cyclop // Fuzzing setup requires multiple type/enum branches
func FuzzSchemaVariableValidation(f *testing.F) {
	f.Add("appName", "string", true, "^[a-z0-9-]+$", "my-app", "app1,app2")
	f.Add("port", "number", true, "^[0-9]+$", "8080", "8080,9090")
	f.Add("enableAuth", "boolean", false, "", "true", "true,false")
	f.Add("filePath", "string", true, "", "../../etc/passwd", "")
	f.Add("maliciousStr", "string", false, "^[A-Z]+$", "invalid_value", "FOO,BAR")
	f.Add("emptyVar", "string", false, "", "", "")

	f.Fuzz(func(t *testing.T, varName, varTypeStr string, required bool, pattern, val, enumVal string) {
		if varName == "" {
			return
		}

		var varType VariableType
		switch varTypeStr {
		case "number":
			varType = TypeNumber
		case "boolean":
			varType = TypeBool
		default:
			varType = TypeString
		}

		var enumVals []string
		if enumVal != "" {
			enumVals = []string{enumVal}
		}

		s := &Schema{
			Description: "Fuzz Test Schema",
			Variables: map[string]VariableDef{
				varName: {
					Type:     varType,
					Required: required,
					Pattern:  pattern,
					Enum:     enumVals,
				},
			},
		}

		metadata := map[string]string{
			varName: val,
		}

		res := Validate(s, metadata)
		if res == nil {
			t.Fatal("Validate returned nil ValidationResult")
		}

		hasErrors := res.HasErrors()
		errMsg := res.Error()
		if hasErrors && errMsg == "" {
			t.Errorf("HasErrors returned true but Error() message was empty")
		}
		if !hasErrors && errMsg != "" {
			t.Errorf("HasErrors returned false but Error() message was %q", errMsg)
		}
	})
}
