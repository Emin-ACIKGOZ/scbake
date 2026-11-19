// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package mavenlinter

import (
	"bytes"
	"testing"
)

// TestEmbeddedTemplatesEndWithNewline covers the snippet template that is
// inserted through InsertXMLTask rather than rendered by CreateTemplateTask, so
// the generic render matrix cannot see its bytes.
func TestEmbeddedTemplatesEndWithNewline(t *testing.T) {
	for _, path := range []string{"checkstyle.xml.tpl", "pom_snippet.xml.tpl"} {
		data, err := templates.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if len(data) == 0 || data[len(data)-1] != '\n' {
			t.Errorf("%s does not end with exactly one newline", path)
		}
		if bytes.HasSuffix(data, []byte("\n\n")) {
			t.Errorf("%s ends with more than one newline", path)
		}
		if bytes.ContainsRune(data, '\r') {
			t.Errorf("%s contains CR bytes; embedded templates must be LF", path)
		}
		for i, line := range bytes.Split(data, []byte("\n")) {
			if bytes.HasSuffix(line, []byte(" ")) || bytes.HasSuffix(line, []byte("\t")) {
				t.Errorf("%s:%d: trailing whitespace", path, i+1)
			}
		}
	}
}
