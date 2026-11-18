// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateXMLFragment covers the strict element-content rules independently
// of the task plumbing.
func TestValidateXMLFragment(t *testing.T) {
	valid := []struct {
		name      string
		snippet   string
		inherited map[string]string
	}{
		{name: "single element", snippet: `<plugin/>`},
		{name: "sibling elements", snippet: `<plugin/><plugin/>`},
		{name: "text", snippet: `plain text`},
		{name: "comment", snippet: `<!-- note -->`},
		{name: "cdata", snippet: `<![CDATA[raw <data> & more]]>`},
		{name: "ordinary processing instruction", snippet: `<?scbake note?>`},
		{name: "bound prefix", snippet: `<x:plugin xmlns:x="urn:x"><x:id>1</x:id></x:plugin>`},
		{name: "default namespace", snippet: `<plugin xmlns="urn:plugin"><id>1</id></plugin>`},
		{name: "default namespace undeclaration", snippet: `<plugin xmlns=""/>`},
		{name: "prefix rebinding", snippet: `<plugin xmlns:x="urn:a"><child xmlns:x="urn:b"><x:id>1</x:id></child></plugin>`},
		{
			name:    "same local attribute name in different namespaces",
			snippet: `<plugin xmlns:a="urn:a" xmlns:b="urn:b" a:id="1" b:id="2"/>`,
		},
		{
			name:    "same local element name in different namespaces",
			snippet: `<plugin xmlns:a="urn:a" xmlns:b="urn:b"><a:id/><b:id/></plugin>`,
		},
		{
			name:    "unprefixed attribute alongside a namespaced one",
			snippet: `<plugin xmlns:a="urn:a" a:id="1" id="2"/>`,
		},
		{
			name:    "reserved xml prefix correctly bound",
			snippet: `<plugin xmlns:xml="http://www.w3.org/XML/1998/namespace"/>`,
		},
		{
			name:      "prefix bound by the enclosing document",
			snippet:   `<x:plugin/>`,
			inherited: map[string]string{"x": "urn:x"},
		},
	}

	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := validateXMLFragment(tc.snippet, tc.inherited); err != nil {
				t.Fatalf("validateXMLFragment(%q) = %v, want nil", tc.snippet, err)
			}
		})
	}

	invalid := []struct {
		name    string
		snippet string
		want    error
	}{
		{"xml declaration", `<?xml version="1.0" encoding="UTF-8"?>`, errXMLDeclarationInContent},
		{"doctype", `<!DOCTYPE project>`, errXMLDirectiveInContent},
		{"unbound element prefix", `<x:plugin/>`, errXMLUnboundPrefix},
		{"unbound attribute prefix", `<plugin x:id="1"/>`, errXMLUnboundPrefix},
		{"unbound nested prefix", `<plugin><x:id>1</x:id></plugin>`, errXMLUnboundPrefix},
		{
			name:    "unbound prefix whose literal matches another prefix's URI",
			snippet: `<z xmlns:y="x"><x:plugin/></z>`,
			want:    errXMLUnboundPrefix,
		},
		{
			name:    "empty prefixed namespace URI",
			snippet: `<plugin xmlns:x=""/>`,
			want:    errXMLInvalidNamespaceDecl,
		},
		{
			name:    "xml prefix bound to another URI",
			snippet: `<plugin xmlns:xml="urn:x"/>`,
			want:    errXMLInvalidNamespaceDecl,
		},
		{
			name:    "xml prefix bound to an empty URI",
			snippet: `<plugin xmlns:xml=""/>`,
			want:    errXMLInvalidNamespaceDecl,
		},
		{
			name:    "xml namespace bound to another prefix",
			snippet: `<plugin xmlns:y="http://www.w3.org/XML/1998/namespace"/>`,
			want:    errXMLInvalidNamespaceDecl,
		},
		{
			name:    "xml namespace bound to the default namespace",
			snippet: `<plugin xmlns="http://www.w3.org/XML/1998/namespace"/>`,
			want:    errXMLInvalidNamespaceDecl,
		},
		{
			name:    "xmlns namespace bound to the default namespace",
			snippet: `<plugin xmlns="http://www.w3.org/2000/xmlns/"/>`,
			want:    errXMLInvalidNamespaceDecl,
		},
		{
			name:    "xmlns namespace bound to a prefix",
			snippet: `<plugin xmlns:y="http://www.w3.org/2000/xmlns/"/>`,
			want:    errXMLInvalidNamespaceDecl,
		},
		{
			name:    "xmlns prefix declared",
			snippet: `<plugin xmlns:xmlns="urn:x"/>`,
			want:    errXMLInvalidNamespaceDecl,
		},
		{
			name:    "attribute name with leading colon",
			snippet: `<plugin :id="1"/>`,
			want:    errXMLInvalidQName,
		},
		{
			name:    "duplicate attribute",
			snippet: `<plugin a="1" a="2"/>`,
			want:    errXMLDuplicateAttribute,
		},
		{
			name:    "duplicate attributes behind one prefix",
			snippet: `<plugin xmlns:p="urn:p" p:a="1" p:a="2"/>`,
			want:    errXMLDuplicateAttribute,
		},
		{
			name:    "duplicate expanded attribute via two prefixes",
			snippet: `<plugin xmlns:a="urn:u" xmlns:b="urn:u" a:id="1" b:id="2"/>`,
			want:    errXMLDuplicateAttribute,
		},
		{
			name:    "duplicate reserved xml prefix attribute",
			snippet: `<plugin xml:lang="en" xml:lang="fr"/>`,
			want:    errXMLDuplicateAttribute,
		},
		{
			name:    "duplicate default namespace declaration",
			snippet: `<plugin xmlns="urn:a" xmlns="urn:b"/>`,
			want:    errXMLDuplicateNamespaceDecl,
		},
		{
			name:    "duplicate prefixed namespace declaration",
			snippet: `<plugin xmlns:p="urn:a" xmlns:p="urn:b"/>`,
			want:    errXMLDuplicateNamespaceDecl,
		},
		{
			name:    "attribute name with trailing colon",
			snippet: `<plugin id:="1"/>`,
			want:    errXMLInvalidQName,
		},
		{
			name:    "element name with leading colon",
			snippet: `<:plugin/>`,
			want:    errXMLInvalidQName,
		},
		{"element name with two colons", `<a:b:c/>`, nil},
		{"attribute name with two colons", `<plugin a:b:c="1"/>`, nil},
		{"malformed", `<plugin><id></plugin>`, nil},
		{"stray closing tag", `</plugin>`, nil},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			err := validateXMLFragment(tc.snippet)
			if err == nil {
				t.Fatalf("validateXMLFragment(%q) = nil, want error", tc.snippet)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("validateXMLFragment(%q) = %v, want %v", tc.snippet, err, tc.want)
			}
		})
	}
}

// TestInsertXMLTask_InvalidSnippetRejected is a regression for #219: a snippet
// that strict parsers reject even though encoding/xml accepts it must fail in
// both dry-run validation and real execution, and must leave pom.xml untouched.
func TestInsertXMLTask_InvalidSnippetRejected(t *testing.T) {
	cases := []struct {
		name    string
		snippet string
	}{
		{"xml declaration", `<?xml version="1.0" encoding="UTF-8"?>`},
		{"doctype", `<!DOCTYPE project>`},
		{"doctype with name", `<!DOCTYPE project SYSTEM "project.dtd">`},
		{"unbound element prefix", `<x:plugin/>`},
		{"unbound attribute prefix", `<plugin x:id="1"/>`},
		{"empty prefixed namespace URI", `<plugin xmlns:x=""/>`},
		{"xml prefix rebound", `<plugin xmlns:xml="urn:x"/>`},
		{"xml namespace on another prefix", `<plugin xmlns:y="http://www.w3.org/XML/1998/namespace"/>`},
		{"default bound to the xmlns namespace", `<plugin xmlns="http://www.w3.org/2000/xmlns/"/>`},
		{"attribute name with leading colon", `<plugin :id="1"/>`},
		{"attribute name with trailing colon", `<plugin id:="1"/>`},
		{"element name with leading colon", `<:plugin/>`},
		{"duplicate attribute", `<plugin a="1" a="2"/>`},
		{"duplicate expanded attribute", `<plugin xmlns:a="urn:x" xmlns:b="urn:x" a:id="1" b:id="2"/>`},
		{"duplicate prefixed namespace declaration", `<plugin xmlns:p="urn:a" xmlns:p="urn:b"/>`},
		{"duplicate default namespace declaration", `<plugin xmlns="urn:a" xmlns="urn:b"/>`},
	}

	for _, tc := range cases {
		tasks := []struct {
			mode string
			run  func(t *testing.T, task *InsertXMLTask, h *testHelper) error
		}{
			{
				mode: "validate dry-run",
				run: func(_ *testing.T, task *InsertXMLTask, h *testHelper) error {
					return task.Validate(h.taskContext(true, false))
				},
			},
			{
				mode: "execute",
				run: func(_ *testing.T, task *InsertXMLTask, h *testHelper) error {
					return task.Execute(h.taskContext(false, false))
				},
			},
			{
				mode: "execute dry-run",
				run: func(_ *testing.T, task *InsertXMLTask, h *testHelper) error {
					return task.Execute(h.taskContext(true, false))
				},
			},
		}

		for _, mode := range tasks {
			t.Run(tc.name+"/"+mode.mode, func(t *testing.T) {
				h := newTestHelper(t)
				before, err := h.readPom()
				if err != nil {
					t.Fatalf("reading fixture: %v", err)
				}

				task := &InsertXMLTask{
					FilePath:    "pom.xml",
					ElementPath: "/project/build/plugins",
					XMLContent:  tc.snippet,
					Desc:        "reject document-level snippet",
					TaskPrio:    1201,
				}

				if err := mode.run(t, task, h); err == nil {
					t.Fatalf("snippet %q was accepted; want an error", tc.snippet)
				}

				after, err := h.readPom()
				if err != nil {
					t.Fatalf("re-reading fixture: %v", err)
				}
				if after != before {
					t.Errorf("failed run modified pom.xml for snippet %q", tc.snippet)
				}
			})
		}
	}
}

// TestInsertXMLTask_FragmentSnippetsAccepted verifies the rule does not reject
// legitimate fragments and that the resulting pom.xml is well-formed and still
// idempotent.
//
//nolint:cyclop // table-driven fragment acceptance cases
func TestInsertXMLTask_FragmentSnippetsAccepted(t *testing.T) {
	cases := []struct {
		name    string
		snippet string
	}{
		{"bound prefix", `<x:plugin xmlns:x="urn:x"><x:id>one</x:id></x:plugin>`},
		{"default namespace", `<plugin xmlns="urn:plugin"><id>one</id></plugin>`},
		{"comment", `<!-- scbake snippet -->`},
		{"cdata", `<![CDATA[raw <data> & more]]>`},
		{"ordinary processing instruction", `<?scbake generated?>`},
		{"multiple siblings", `<plugin><id>a</id></plugin><plugin><id>b</id></plugin>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHelper(t)
			before, err := h.readPom()
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}

			// The snippet is element content by the strict fragment rules.
			if err := validateXMLFragment(tc.snippet); err != nil {
				t.Fatalf("valid snippet rejected by validateXMLFragment: %v", err)
			}

			task := &InsertXMLTask{
				FilePath:    "pom.xml",
				ElementPath: "/project/build/plugins",
				XMLContent:  tc.snippet,
				Desc:        "accept fragment snippet",
				TaskPrio:    1201,
			}
			tcCtx := h.taskContext(false, false)

			if err := task.Execute(tcCtx); err != nil {
				t.Fatalf("Execute() = %v, want nil", err)
			}

			after, err := h.readPom()
			if err != nil {
				t.Fatalf("re-reading fixture: %v", err)
			}
			if after == before {
				t.Fatalf("pom.xml unchanged after inserting %q", tc.snippet)
			}
			if !strings.Contains(after, strings.TrimSpace(tc.snippet)) {
				t.Errorf("inserted snippet %q not found in pom.xml", tc.snippet)
			}

			// The written document must satisfy both the plain well-formedness
			// check and the strict declaration/namespace rules.
			if err := validateXML(after); err != nil {
				t.Errorf("resulting pom.xml is malformed: %v", err)
			}
			if err := validateXMLDocument(after); err != nil {
				t.Errorf("resulting pom.xml is invalid element content: %v", err)
			}

			// A second run is a no-op.
			if err := task.Execute(tcCtx); err != nil {
				t.Fatalf("second Execute() = %v, want nil", err)
			}
			again, err := h.readPom()
			if err != nil {
				t.Fatalf("re-reading fixture: %v", err)
			}
			if again != after {
				t.Error("second execution modified pom.xml (idempotency violation)")
			}
		})
	}
}

// TestInsertXMLElement_RejectsDocumentLevelOutput exercises the splice sink
// directly, bypassing the task's snippet validation.
func TestInsertXMLElement_RejectsDocumentLevelOutput(t *testing.T) {
	file := `<project><build><plugins></plugins></build></project>`

	bad := []string{
		`<?xml version="1.0"?>`,
		`<!DOCTYPE project>`,
		`<x:plugin/>`,
		`<plugin x:id="1"/>`,
		`<plugin xmlns:x=""/>`,
		`<plugin xmlns:xml="urn:x"/>`,
		`<plugin xmlns:y="http://www.w3.org/XML/1998/namespace"/>`,
		`<plugin xmlns="http://www.w3.org/2000/xmlns/"/>`,
		`<plugin :id="1"/>`,
		`<plugin id:="1"/>`,
		`<:plugin/>`,
		`<plugin a="1" a="2"/>`,
		`<plugin xmlns:a="urn:x" xmlns:b="urn:x" a:id="1" b:id="2"/>`,
		`<plugin xmlns:p="urn:a" xmlns:p="urn:b"/>`,
		`<plugin xmlns="urn:a" xmlns="urn:b"/>`,
	}

	for _, snippet := range bad {
		t.Run(snippet, func(t *testing.T) {
			if _, err := insertXMLElement(file, "/project/build/plugins", snippet); err == nil {
				t.Fatalf("insertXMLElement() accepted %q, want error", snippet)
			}
		})
	}
}
