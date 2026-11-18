// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// InsertXMLTask inserts an XML fragment into an existing XML file at a specified path.
// It safely handles file modifications with transaction tracking for rollback.
type InsertXMLTask struct {
	// FilePath is the path to the XML file (relative to TargetPath)
	FilePath string

	// ElementPath is a simplified XPath indicating where to insert (e.g., "/project/build/plugins")
	ElementPath string

	// XMLContent is the XML fragment to insert (must be valid element content)
	XMLContent string

	Desc string

	TaskPrio int
}

// Description implements types.Task.
func (t *InsertXMLTask) Description() string {
	return t.Desc
}

// Priority implements types.Task.
func (t *InsertXMLTask) Priority() int {
	return t.TaskPrio
}

// xmlInsertPlan is the read-only outcome of preparing an insertion. It is what
// Execute would do, computed without tracking or writing.
type xmlInsertPlan struct {
	// absPath is the resolved insertion target.
	absPath string
	// spliced is the file content with the fragment inserted. It is empty when
	// the fragment is already present or insertion is deferred.
	spliced string
	// alreadyInserted reports that the fragment is present, so writing is a
	// no-op.
	alreadyInserted bool
	// deferred reports that the target does not exist yet but an earlier task
	// in the same plan is expected to create it. It is only true in a dry run.
	deferred bool
}

// errXMLFileNotFound marks a missing insertion target so prepareInsert can tell
// a plain missing file from one a preceding task is expected to create. The
// wrapped message keeps the historic "XML file not found: <path>" text.
var errXMLFileNotFound = errors.New("XML file not found")

// prepareInsert performs every read-only check an insertion makes: target path
// safety, element path syntax, XML well-formedness of the file and the snippet,
// and whether the snippet is already present or the insertion point exists. It
// returns the plan and never tracks or writes; it is shared by Validate and
// Execute so the preview reports the real decision.
func (t *InsertXMLTask) prepareInsert(tc types.TaskContext) (xmlInsertPlan, error) {
	absPath, err := t.validatePath(tc.TargetPath)
	if err != nil {
		plan, handled, deferredErr := t.deferredInsertPlan(tc, absPath, err)
		if handled {
			return plan, deferredErr
		}
		return xmlInsertPlan{}, err
	}

	if _, err := parseElementPath(t.ElementPath); err != nil {
		return xmlInsertPlan{}, err
	}

	contentStr, err := t.readAndValidateXML(tc.TargetPath)
	if err != nil {
		return xmlInsertPlan{}, err
	}

	if t.isAlreadyInserted(contentStr) {
		return xmlInsertPlan{absPath: absPath, alreadyInserted: true}, nil
	}

	// Validate the insertion point in memory. Nothing is written, but a missing
	// or self-closing target fails exactly as the apply path would. The spliced
	// content is carried into Execute so the real run does not parse again.
	spliced, err := insertXMLElement(contentStr, t.ElementPath, t.XMLContent)
	if err != nil {
		return xmlInsertPlan{}, fmt.Errorf("failed to insert XML into %s: %w", t.FilePath, err)
	}

	return xmlInsertPlan{absPath: absPath, spliced: spliced}, nil
}

// deferredInsertPlan handles a target that does not exist yet. When a preceding
// task in the same plan declared it, a dry run validates the parts that do not
// depend on the file and defers the rest; handled is false when the path error
// must stand (a plain missing file, or a non-dry run).
func (t *InsertXMLTask) deferredInsertPlan(
	tc types.TaskContext, absPath string, pathErr error,
) (plan xmlInsertPlan, handled bool, err error) {
	if !tc.DryRun || !errors.Is(pathErr, errXMLFileNotFound) || !tc.PathPlanned(absPath) {
		return xmlInsertPlan{}, false, nil
	}
	if _, err := parseElementPath(t.ElementPath); err != nil {
		return xmlInsertPlan{}, true, err
	}
	if err := t.validateSnippet(); err != nil {
		return xmlInsertPlan{}, true, err
	}
	return xmlInsertPlan{absPath: absPath, deferred: true}, true, nil
}

// Validate checks the target path, element path, file content, and insertion
// point without tracking or writing.
func (t *InsertXMLTask) Validate(tc types.TaskContext) error {
	_, err := t.prepareInsert(tc)
	return err
}

// Execute performs the XML insertion with safety checks and transaction tracking.
func (t *InsertXMLTask) Execute(tc types.TaskContext) error {
	plan, err := t.prepareInsert(tc)
	if err != nil {
		return err
	}

	if tc.DryRun {
		return nil
	}

	if plan.deferred {
		// prepareInsert only defers in a dry run; a real run must still fail
		// when the target was never created.
		return fmt.Errorf("%w: %s", errXMLFileNotFound, t.FilePath)
	}

	if err := t.trackFile(tc, plan.absPath); err != nil {
		return err
	}

	if plan.alreadyInserted {
		return nil
	}

	return t.insertAndWrite(tc, plan.absPath, plan.spliced)
}

// insertAndWrite records the intended bytes for rollback and writes the content
// prepared by prepareInsert.
func (t *InsertXMLTask) insertAndWrite(tc types.TaskContext, absPath, newContent string) error {
	// If the file vanished between Track and this write, record the intended
	// bytes so rollback can verify and remove only exactly what was written.
	if err := trackXMLWrite(tc, absPath, t.FilePath, []byte(newContent)); err != nil {
		return err
	}

	if err := fileutil.SafeWriteFile(tc.TargetPath, t.FilePath, []byte(newContent), fileutil.FilePerms); err != nil {
		return fmt.Errorf("failed to write modified XML file %s: %w", t.FilePath, err)
	}

	return nil
}

// trackXMLWrite registers the intended XML bytes when a transaction is active.
func trackXMLWrite(tc types.TaskContext, absPath, relPath string, content []byte) error {
	if tc.Tx == nil {
		return nil
	}
	if err := tc.Tx.TrackWrite(absPath, content); err != nil {
		return fmt.Errorf("failed to track file %s: %w", relPath, err)
	}
	return nil
}

func (t *InsertXMLTask) validatePath(targetPath string) (string, error) {
	// fileutil.SafePath performs both lexical and symlink-aware containment, so
	// a file reached through a symlink that escapes the target is rejected.
	absPath, err := fileutil.SafePath(targetPath, t.FilePath)
	if err != nil {
		return "", fmt.Errorf("path %s is outside target directory: %w", t.FilePath, err)
	}

	// Lstat: do not follow a symlink. The final component is later opened with
	// O_NOFOLLOW, but rejecting here reports a clear error and prevents reading
	// through a link.
	if info, err := os.Lstat(absPath); err != nil {
		if os.IsNotExist(err) {
			// Return the resolved path so the caller can check whether an
			// earlier task is expected to create it.
			return absPath, fmt.Errorf("%w: %s", errXMLFileNotFound, t.FilePath)
		}
		return "", fmt.Errorf("failed to stat XML file %s: %w", t.FilePath, err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("XML file %s is a symlink, which is not allowed", t.FilePath)
	}

	return absPath, nil
}

func (t *InsertXMLTask) readAndValidateXML(targetPath string) (string, error) {
	// The path is validated by validatePath() and SafeReadFile re-checks
	// containment, opening the final component with O_NOFOLLOW.
	content, err := fileutil.SafeReadFile(targetPath, t.FilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read XML file %s: %w", t.FilePath, err)
	}

	contentStr := string(content)
	if err := validateXML(contentStr); err != nil {
		return "", fmt.Errorf("XML file %s is malformed: %w", t.FilePath, err)
	}

	// A snippet may rely on a prefix bound by the target document, so validate
	// it against the namespace scope actually in effect at the insertion point.
	// A prefix rebound by a non-ancestor element must not leak into that scope:
	// the document-wide declaration map takes the last declaration anywhere in
	// the file and can disagree with the splice result.
	segments, err := parseElementPath(t.ElementPath)
	if err != nil {
		return "", err
	}
	inherited := xmlNamespaceDeclarations(contentStr)
	_, scope, locateErr := locateXMLElementWithScope(contentStr, segments)
	if locateErr == nil {
		inherited = scope
	}
	// When the target cannot be located, keep the historical document-wide
	// fallback: the splice path reports the missing or self-closing target, and
	// validateXMLDocument(output) remains the authoritative check.
	if err := t.validateSnippet(inherited); err != nil {
		return "", err
	}

	return contentStr, nil
}

// validateSnippet checks that the fragment to insert is well-formed XML and
// valid element content. The optional declared map holds namespace prefixes the
// target document declares; when it is omitted (a deferred dry run, where the
// document is not available) the fragment must be self-contained.
func (t *InsertXMLTask) validateSnippet(declared ...map[string]string) error {
	if err := validateXML(t.XMLContent); err != nil {
		return fmt.Errorf("XML content to insert is malformed: %w", err)
	}
	if err := validateXMLFragment(t.XMLContent, declared...); err != nil {
		return fmt.Errorf("XML content to insert is not valid element content: %w", err)
	}
	return nil
}

func (t *InsertXMLTask) trackFile(tc types.TaskContext, absPath string) error {
	if tc.Tx != nil {
		if err := tc.Tx.Track(absPath); err != nil {
			return fmt.Errorf("failed to track file %s: %w", t.FilePath, err)
		}
	}
	return nil
}

func (t *InsertXMLTask) isAlreadyInserted(contentStr string) bool {
	normalizedNew := strings.TrimSpace(t.XMLContent)
	return containsNormalizedXML(contentStr, normalizedNew)
}

// containsNormalizedXML checks if normalized XML content exists in file (ignores whitespace differences).
// Uses O(n) single-pass algorithm instead of nested loops.
func containsNormalizedXML(fileContent, xmlSnippet string) bool {
	normalizedSnippet := strings.TrimSpace(xmlSnippet)
	if normalizedSnippet == "" {
		return false
	}

	// Split into lines once
	snippetLines := strings.Split(normalizedSnippet, "\n")
	if len(snippetLines) == 0 {
		return false
	}

	// Normalize snippet lines once
	normalizedSnippetLines := make([]string, len(snippetLines))
	for i, line := range snippetLines {
		normalizedSnippetLines[i] = strings.TrimSpace(line)
	}

	// Split file into lines
	fileLines := strings.Split(fileContent, "\n")
	if len(fileLines) < len(normalizedSnippetLines) {
		return false
	}

	// Single pass through file
	for i := 0; i <= len(fileLines)-len(normalizedSnippetLines); i++ {
		match := true
		for j, snippetLine := range normalizedSnippetLines {
			if strings.TrimSpace(fileLines[i+j]) != snippetLine {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}

	return false
}

// validateXML checks that the given string is well-formed XML.
func validateXML(content string) error {
	decoder := xml.NewDecoder(strings.NewReader(content))
	// Set strict mode to catch malformed XML
	decoder.Strict = true
	for {
		_, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

const (
	// xmlFragmentRoot is the synthetic wrapper used to validate a fragment as
	// element content rather than as a standalone document.
	xmlFragmentRoot = "scbake-fragment"
	// xmlNamespaceURI is the reserved namespace bound to the "xml" prefix.
	xmlNamespaceURI = "http://www.w3.org/XML/1998/namespace"
	// xmlPrefix is the reserved prefix bound to xmlNamespaceURI.
	xmlPrefix = "xml"
	// xmlnsPrefix is the pseudo-prefix the decoder reports for namespace
	// declaration attributes such as xmlns:name="uri".
	xmlnsPrefix = "xmlns"
	// xmlnsNamespaceURI is the reserved namespace of xmlns declarations. No
	// prefix may be bound to it.
	xmlnsNamespaceURI = "http://www.w3.org/2000/xmlns/"
)

// errXMLDeclarationInContent marks an XML declaration found where only element
// content is allowed.
var errXMLDeclarationInContent = errors.New("XML declaration is not allowed in element content")

// errXMLDirectiveInContent marks a directive (DOCTYPE, entity declaration)
// found where only element content is allowed.
var errXMLDirectiveInContent = errors.New("XML directive is not allowed in element content")

// errXMLUnboundPrefix marks a prefixed name whose prefix is declared nowhere in
// scope.
var errXMLUnboundPrefix = errors.New("unbound namespace prefix")

// errXMLInvalidNamespaceDecl marks an xmlns declaration that binds a reserved
// name or an invalid URI.
var errXMLInvalidNamespaceDecl = errors.New("invalid XML namespace declaration")

// errXMLInvalidQName marks a raw element or attribute name that is not a valid
// XML qualified name.
var errXMLInvalidQName = errors.New("invalid XML qualified name")

// errXMLDuplicateAttribute marks two attributes on one element whose expanded
// names (namespace URI and local name) are the same. XML requires attribute
// names to be unique after namespace expansion, not merely as written.
var errXMLDuplicateAttribute = errors.New("duplicate XML attribute")

// errXMLDuplicateNamespaceDecl marks two declarations of the same prefix (or of
// the default namespace) on one element.
var errXMLDuplicateNamespaceDecl = errors.New("duplicate XML namespace declaration")

// validateXMLFragment checks that content is well-formed XML *element content*:
// text, comments, CDATA, processing instructions, and one or more sibling
// elements, as accepted at an insertion point inside an existing element.
//
// The fragment is wrapped in a synthetic root so the decoder can parse sibling
// elements. Document-level constructs are then rejected: an XML declaration is
// not element content, and a directive (DOCTYPE, entity declaration) is not
// either. Namespaces are checked against the declarations made in the fragment
// plus any inherited declarations supplied by the caller, because a snippet may
// use a prefix its target document binds on an ancestor. A name whose namespace
// is not declared anywhere in that scope is an unbound prefix.
func validateXMLFragment(content string, inherited ...map[string]string) error {
	wrapped := "<" + xmlFragmentRoot + ">" + content + "</" + xmlFragmentRoot + ">"

	scope := map[string]string{xmlPrefix: xmlNamespaceURI}
	for _, decls := range inherited {
		for prefix, uri := range decls {
			if _, exists := scope[prefix]; !exists {
				scope[prefix] = uri
			}
		}
	}

	return walkXMLNamespaces(wrapped, scope, false)
}

// validateXMLDocument applies the fragment-level rules to a complete document.
// Unlike a fragment, a document may carry an XML declaration and directives,
// but only before its root element; anything document-level after an element
// has started would be content injected into the tree and is rejected.
func validateXMLDocument(content string) error {
	return walkXMLNamespaces(content, map[string]string{xmlPrefix: xmlNamespaceURI}, true)
}

// walkXMLNamespaces token-walks content and enforces the declaration and
// namespace rules described on validateXMLFragment. When document is true the
// prolog may contain an XML declaration and directives before the root element.
func walkXMLNamespaces(content string, base map[string]string, document bool) error {
	if err := walkXMLDeclarations(content, base, document); err != nil {
		return err
	}
	// The decoder resolves a bound prefix to its namespace URI and reports an
	// unbound prefix as the literal prefix. A separate raw walk checks prefix
	// declarations exactly, so a URI that happens to equal an undeclared prefix
	// cannot make an unbound prefix look bound.
	return walkXMLPrefixes(content, base)
}

// walkXMLDeclarations rejects document-level constructs in element content and
// checks that every resolved namespace URI is declared in scope.
//
//nolint:cyclop // One linear token walk with one case per token type
func walkXMLDeclarations(content string, base map[string]string, document bool) error {
	decoder := xml.NewDecoder(strings.NewReader(content))
	decoder.Strict = true

	scopes := []map[string]string{base}
	rootStarted := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		switch e := tok.(type) {
		case xml.ProcInst:
			if !strings.EqualFold(e.Target, xmlPrefix) {
				// Ordinary processing instructions are element content.
				continue
			}
			if document && !rootStarted {
				// The document's own XML declaration.
				continue
			}
			return errXMLDeclarationInContent

		case xml.Directive:
			if document && !rootStarted {
				// A document type declaration belongs in the prolog.
				continue
			}
			return fmt.Errorf("%w: %q", errXMLDirectiveInContent, string(e))

		case xml.StartElement:
			scope := copyXMLNS(scopes[len(scopes)-1])
			for _, a := range e.Attr {
				switch {
				case a.Name.Space == xmlnsPrefix:
					if err := checkXMLNamespaceDeclaration(a.Name.Local, a.Value); err != nil {
						return err
					}
					scope[a.Name.Local] = a.Value
				case a.Name.Space == "" && a.Name.Local == xmlnsPrefix:
					if err := checkXMLNamespaceDeclaration("", a.Value); err != nil {
						return err
					}
					scope[""] = a.Value
				}
			}
			scopes = append(scopes, scope)
			rootStarted = true

			if err := checkXMLNamespace("element", e.Name, scope); err != nil {
				return err
			}
			for _, a := range e.Attr {
				if isXMLNSAttr(a.Name) {
					continue
				}
				if err := checkXMLNamespace("attribute", a.Name, scope); err != nil {
					return err
				}
			}

		case xml.EndElement:
			if len(scopes) > 1 {
				scopes = scopes[:len(scopes)-1]
			}
		}
	}
}

// walkXMLPrefixes verifies that every prefixed element and attribute name uses
// a prefix declared in scope, and that no element carries two attributes with
// the same expanded name. RawToken is used because Token has already resolved
// real declarations to URIs, which makes an undeclared prefix indistinguishable
// from a URI in some contrived documents, and because it preserves the raw
// prefix needed to expand each attribute name.
//
//nolint:cyclop // linear raw-token namespace walk
func walkXMLPrefixes(content string, base map[string]string) error {
	decoder := xml.NewDecoder(strings.NewReader(content))
	decoder.Strict = true

	scopes := []map[string]string{copyXMLNS(base)}
	for {
		tok, err := decoder.RawToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		switch e := tok.(type) {
		case xml.StartElement:
			scope := copyXMLNS(scopes[len(scopes)-1])
			if err := collectXMLNamespaceDeclarations(e, scope); err != nil {
				return err
			}
			scopes = append(scopes, scope)

			if err := checkXMLQName("element", e.Name); err != nil {
				return err
			}
			if err := checkXMLPrefix("element", e.Name, scope); err != nil {
				return err
			}
			if err := checkXMLAttributeNames(e, scope); err != nil {
				return err
			}

		case xml.EndElement:
			if len(scopes) > 1 {
				scopes = scopes[:len(scopes)-1]
			}
		}
	}
}

// collectXMLNamespaceDeclarations validates the raw names and declarations of
// one start tag, records the declarations in scope for the element, and rejects
// the same prefix (or the default namespace) being declared twice on it.
func collectXMLNamespaceDeclarations(e xml.StartElement, scope map[string]string) error {
	declared := make(map[string]struct{}, len(e.Attr))
	for _, a := range e.Attr {
		if err := checkXMLQName("attribute", a.Name); err != nil {
			return err
		}

		prefix, isDeclaration := "", false
		switch {
		case a.Name.Space == xmlnsPrefix:
			prefix, isDeclaration = a.Name.Local, true
		case a.Name.Space == "" && a.Name.Local == xmlnsPrefix:
			prefix, isDeclaration = "", true
		}
		if !isDeclaration {
			continue
		}

		if err := checkXMLNamespaceDeclaration(prefix, a.Value); err != nil {
			return err
		}
		if _, duplicate := declared[prefix]; duplicate {
			return fmt.Errorf("%w for prefix %q on element %q", errXMLDuplicateNamespaceDecl, prefix, rawXMLName(e.Name))
		}
		declared[prefix] = struct{}{}
		scope[prefix] = a.Value
	}
	return nil
}

// checkXMLAttributeNames verifies that every non-declaration attribute has a
// declared prefix and that no two attributes expand to the same namespace URI
// and local name. Two prefixes bound to one URI therefore collide, while the
// same local name in different namespaces remains legal. An unprefixed
// attribute is in no namespace: the default namespace never applies to it.
func checkXMLAttributeNames(e xml.StartElement, scope map[string]string) error {
	expanded := make(map[xmlExpandedName]struct{}, len(e.Attr))
	for _, a := range e.Attr {
		if isXMLNSAttr(a.Name) {
			continue
		}
		if err := checkXMLPrefix("attribute", a.Name, scope); err != nil {
			return err
		}
		name, bound := expandXMLAttributeName(a.Name, scope)
		if !bound {
			// checkXMLPrefix already rejected an undeclared prefix.
			continue
		}
		if _, duplicate := expanded[name]; duplicate {
			return fmt.Errorf("%w %q on element %q", errXMLDuplicateAttribute, rawXMLName(a.Name), rawXMLName(e.Name))
		}
		expanded[name] = struct{}{}
	}
	return nil
}

// xmlExpandedName is an attribute name after namespace expansion.
type xmlExpandedName struct {
	space string
	local string
}

// expandXMLAttributeName resolves a raw attribute name against the in-scope
// prefix map. Unprefixed attributes have no namespace, and the reserved xml
// prefix always denotes xmlNamespaceURI. bound is false for an undeclared
// prefix, which checkXMLPrefix reports.
func expandXMLAttributeName(name xml.Name, scope map[string]string) (xmlExpandedName, bool) {
	if name.Space == "" {
		return xmlExpandedName{local: name.Local}, true
	}
	if name.Space == xmlPrefix {
		return xmlExpandedName{space: xmlNamespaceURI, local: name.Local}, true
	}
	uri, declared := scope[name.Space]
	if !declared {
		return xmlExpandedName{}, false
	}
	return xmlExpandedName{space: uri, local: name.Local}, true
}

// checkXMLPrefix rejects a raw name whose prefix was not declared in scope.
func checkXMLPrefix(kind string, name xml.Name, scope map[string]string) error {
	if name.Space == "" || name.Space == xmlPrefix {
		return nil
	}
	if _, declared := scope[name.Space]; declared {
		return nil
	}
	return fmt.Errorf("%w %q used by %s %q", errXMLUnboundPrefix, name.Space, kind, name.Local)
}

// checkXMLNamespaceDeclaration rejects declarations that XML Namespaces forbids.
// prefix is empty for the default namespace. An empty default namespace is a
// legal undeclaration; an empty prefixed namespace is not. The xml prefix is
// reserved and must always denote xmlNamespaceURI, no other prefix may denote
// it, and nothing may bind the xmlns namespace.
func checkXMLNamespaceDeclaration(prefix, uri string) error {
	switch {
	case prefix == xmlPrefix:
		if uri != xmlNamespaceURI {
			return fmt.Errorf("%w: prefix %q must be bound to %q", errXMLInvalidNamespaceDecl, prefix, xmlNamespaceURI)
		}
	case uri == xmlNamespaceURI:
		return fmt.Errorf("%w: prefix %q must not be bound to the reserved XML namespace", errXMLInvalidNamespaceDecl, prefix)
	case uri == xmlnsNamespaceURI:
		return fmt.Errorf("%w: prefix %q must not be bound to the reserved xmlns namespace", errXMLInvalidNamespaceDecl, prefix)
	case prefix == xmlnsPrefix:
		return fmt.Errorf("%w: the %q prefix must not be declared", errXMLInvalidNamespaceDecl, xmlnsPrefix)
	case prefix != "" && uri == "":
		return fmt.Errorf("%w: prefix %q must not be bound to an empty URI", errXMLInvalidNamespaceDecl, prefix)
	}
	return nil
}

// checkXMLQName rejects malformed raw qualified names. RawToken leaves the raw
// prefix in Space and the raw local part in Local, so a leading colon survives
// in Local, a trailing colon leaves Local empty, and extra colons survive inside
// one of the parts. A QName is an optional non-empty prefix, a colon, and a
// non-empty local part, and neither part may contain a colon.
func checkXMLQName(kind string, name xml.Name) error {
	if name.Local == "" || strings.ContainsRune(name.Local, ':') {
		return fmt.Errorf("%w: %s name %q", errXMLInvalidQName, kind, rawXMLName(name))
	}
	if name.Space != "" && strings.ContainsRune(name.Space, ':') {
		return fmt.Errorf("%w: %s name %q", errXMLInvalidQName, kind, rawXMLName(name))
	}
	return nil
}

// rawXMLName reassembles a name from its raw prefix and local part for errors.
func rawXMLName(name xml.Name) string {
	if name.Space == "" {
		return name.Local
	}
	return name.Space + ":" + name.Local
}

// checkXMLNamespace rejects a name whose namespace is not declared in scope.
// The decoder resolves a bound prefix to its namespace URI and reports an
// unbound prefix literally, so a non-empty Space must either be a URI declared
// by an xmlns attribute or a prefix the caller inherited from the enclosing
// document. The reserved "xml" prefix is always in scope. The empty namespace
// (unprefixed names, including the default namespace) is accepted.
func checkXMLNamespace(kind string, name xml.Name, scope map[string]string) error {
	if name.Space == "" || name.Space == xmlPrefix || name.Space == xmlNamespaceURI {
		return nil
	}
	if uri, declared := scope[name.Space]; declared && uri != "" {
		return nil
	}
	for _, uri := range scope {
		if uri == name.Space {
			return nil
		}
	}
	return fmt.Errorf("%w %q used by %s %q", errXMLUnboundPrefix, name.Space, kind, name.Local)
}

// isXMLNSAttr reports whether a name is a namespace declaration attribute.
func isXMLNSAttr(name xml.Name) bool {
	return name.Space == xmlnsPrefix || (name.Space == "" && name.Local == xmlnsPrefix)
}

// xmlNamespaceDeclarations collects the prefix declarations made anywhere in a
// well-formed document. It is used to let a snippet use a prefix the target
// document binds on an ancestor. Declarations that are not in scope at the
// insertion point are harmless here: the spliced output is validated with real
// scoping, so an unbound prefix is still rejected before any write.
func xmlNamespaceDeclarations(content string) map[string]string {
	decls := make(map[string]string)
	decoder := xml.NewDecoder(strings.NewReader(content))
	decoder.Strict = true
	for {
		tok, err := decoder.Token()
		if err != nil {
			return decls
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, a := range start.Attr {
			switch {
			case a.Name.Space == xmlnsPrefix:
				decls[a.Name.Local] = a.Value
			case a.Name.Space == "" && a.Name.Local == xmlnsPrefix:
				decls[""] = a.Value
			}
		}
	}
}

// parseElementPath splits a simplified element path (e.g. "/project/build/plugins")
// into its segments. Empty segments (from repeated slashes) are ignored; a path
// with no segments at all is rejected.
func parseElementPath(elementPath string) ([]string, error) {
	var segments []string
	for _, s := range strings.Split(strings.Trim(elementPath, "/"), "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("invalid element path: %q", elementPath)
	}
	return segments, nil
}

// xmlFrame tracks one open element while locating an insertion point.
type xmlFrame struct {
	// name is the element name as reported by xml.Decoder.Token (Space is the
	// namespace URL, Local the local name).
	name xml.Name
	// ns is the scoped namespace environment (prefix → URL, "" for the default
	// namespace) in effect for this element.
	ns map[string]string
	// matched is true when this element and all of its ancestors matched the
	// requested path.
	matched bool
}

// locateXMLElement walks the XML structure of content and returns the byte
// offset at which an insertion must be spliced: immediately before the closing
// tag of the first element whose full ancestry matches segments.
//
// Namespace handling is the repository's ElementPath contract, not XPath: a
// prefixed segment (prefix:local) resolves its prefix against the scoped xmlns
// environment; an unprefixed segment matches the default namespace in scope.
//
// Self-closing targets are rejected: Token() expands them into a synthesized
// EndElement that does not advance the input position, so its token span is
// zero-width. This is an observed property of the decoder and is covered by
// TestInsertXMLElement_SelfClosingTargetOffsets.
func locateXMLElement(content string, segments []string) (int64, error) {
	insertAt, _, err := locateXMLElementWithScope(content, segments)
	return insertAt, err
}

// locateXMLElementWithScope is locateXMLElement plus the prefix→URI map that is
// in scope inside the matched target element ("" is the default namespace).
// Snippets inserted there see exactly this scope, including declarations on the
// target element itself; a declaration made by a non-ancestor does not apply.
//
//nolint:cyclop // Linear structural walk with per-token checks
func locateXMLElementWithScope(content string, segments []string) (int64, map[string]string, error) {
	dec := xml.NewDecoder(strings.NewReader(content))

	var stack []xmlFrame
	for {
		// InputOffset before reading a token is the start of that token and
		// the end of the previously returned one.
		closeStart := dec.InputOffset()
		tok, err := dec.Token()
		closeEnd := dec.InputOffset()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, nil, fmt.Errorf("failed to parse XML: %w", err)
		}

		switch e := tok.(type) {
		case xml.StartElement:
			frame := xmlFrame{name: e.Name}
			if len(stack) > 0 {
				frame.ns = copyXMLNS(stack[len(stack)-1].ns)
			} else {
				frame.ns = make(map[string]string)
			}
			for _, a := range e.Attr {
				switch {
				case a.Name.Space == xmlnsPrefix:
					frame.ns[a.Name.Local] = a.Value
				case a.Name.Space == "" && a.Name.Local == xmlnsPrefix:
					frame.ns[""] = a.Value
				}
			}
			if len(stack) < len(segments) &&
				(len(stack) == 0 || stack[len(stack)-1].matched) {
				frame.matched = segmentMatches(segments[len(stack)], frame.ns, e.Name)
			}
			stack = append(stack, frame)

		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			if top.matched && len(stack) == len(segments) {
				// A synthesized EndElement (from a self-closing tag) does not
				// consume input, so its span is zero-width. An empty element
				// such as <a></a> also has closeStart == openEnd, but its end
				// tag does advance the decoder.
				if closeStart == closeEnd {
					return 0, nil, fmt.Errorf("cannot insert into self-closing element <%s>", top.name.Local)
				}
				return closeStart, copyXMLNS(top.ns), nil
			}
			stack = stack[:len(stack)-1]
		}
	}

	return 0, nil, fmt.Errorf("target element path /%s not found", strings.Join(segments, "/"))
}

// segmentMatches reports whether a path segment matches an element name under
// the scoped namespace environment (see locateXMLElement for the contract).
func segmentMatches(segment string, ns map[string]string, name xml.Name) bool {
	local := segment
	space := ns[""]
	if idx := strings.IndexByte(segment, ':'); idx >= 0 {
		prefix := segment[:idx]
		local = segment[idx+1:]

		// An undeclared prefix must never fall back to the default or empty
		// namespace; only explicit declarations resolve.
		resolved, ok := ns[prefix]
		if !ok {
			return false
		}
		space = resolved
	}
	return name.Local == local && name.Space == space
}

func copyXMLNS(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// insertXMLElement finds the target element path and inserts the XML content within it.
// For example, with path "/project/build/plugins" and existing file, it locates the
// <plugins> element structurally via the token stream and splices the new XML fragment
// into the original source just before </plugins>.
func insertXMLElement(fileContent, elementPath, xmlToInsert string) (string, error) {
	segments, err := parseElementPath(elementPath)
	if err != nil {
		return "", err
	}

	insertAt, err := locateXMLElement(fileContent, segments)
	if err != nil {
		return "", err
	}
	// Validate the calculated offset before slicing so malformed or unexpected
	// input can never cause an out-of-range slice.
	if insertAt < 0 || insertAt > int64(len(fileContent)) {
		return "", fmt.Errorf("invalid insertion offset %d for path %s", insertAt, elementPath)
	}

	// Splice the new content before the closing tag of the target element.
	before := fileContent[:insertAt]
	after := fileContent[insertAt:]

	// Add proper indentation to the inserted content
	insertedLines := strings.Split(strings.TrimSpace(xmlToInsert), "\n")
	var result strings.Builder
	result.WriteString(before)
	result.WriteString("\n    ")
	for i, line := range insertedLines {
		result.WriteString(strings.TrimSpace(line))
		if i < len(insertedLines)-1 {
			result.WriteString("\n    ")
		}
	}
	result.WriteString("\n")
	result.WriteString(after)

	output := result.String()

	// Validate the spliced document before returning it. The fragment checks in
	// the task catch bad snippets early, but insertXMLElement is also a direct
	// entry point; re-validating the output guarantees a caller can never write
	// an XML declaration, directive, or unbound prefix into an element.
	if err := validateXML(output); err != nil {
		return "", fmt.Errorf("insertion would produce malformed XML: %w", err)
	}
	if err := validateXMLDocument(output); err != nil {
		return "", fmt.Errorf("insertion would produce invalid element content: %w", err)
	}

	return output, nil
}
