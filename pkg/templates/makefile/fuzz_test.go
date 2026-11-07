// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package makefile

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"text/template"

	"scbake/internal/types"
)

// fuzzTemplateFuncs intentionally mirrors only the custom helpers the
// production renderer installs in pkg/tasks. tasks.templateFuncs is unexported,
// and the Makefile template currently calls only quote; reusing the production
// map would require adding an export solely for tests.
var fuzzTemplateFuncs = template.FuncMap{
	"quote": func(s string) string { return fmt.Sprintf("%q", s) },
}

// emptySubshellRecipe matches a rendered recipe line whose subshell has no
// command after `&&`, e.g. `(cd . && )`. Anchoring on a recipe line avoids
// treating the same bytes inside an `echo` argument as a command, and the
// required whitespace keeps `&&)` inside a language suffix such as
// `$(MAKE_BUILD_COMMAND_&&)` from matching a form that is not an empty
// command.
var emptySubshellRecipe = regexp.MustCompile(`(?m)^[ \t]*\(cd[^\n]*&&[ \t]+\)`)

// emptyLanguageTestRef matches an empty per-language test variable used as a
// recipe command, e.g. `(cd . && $(MAKE_TEST_COMMAND_))`.
var emptyLanguageTestRef = regexp.MustCompile(`(?m)^[ \t]*\(cd[^\n]*\$\(MAKE_TEST_COMMAND_\)`)

// FuzzMakefileTestTarget renders the embedded Makefile template in memory for
// arbitrary manifest data and asserts the #148 test-target contract:
//
//  1. rendering never errors or panics for arbitrary string data;
//  2. no recipe contains an empty subshell `(cd ... && )` or the empty-suffix
//     variable `$(MAKE_TEST_COMMAND_)`;
//  3. a present, non-empty, non-legacy test_command is used verbatim and
//     suppresses the per-language test loop;
//  4. no projects and no usable root command yields a clear failing target;
//  5. supported languages (go/svelte/spring) emit their
//     MAKE_TEST_COMMAND_<lang> recipe, while unsupported/custom/empty
//     languages emit only the neutral skip line.
func FuzzMakefileTestTarget(f *testing.F) {
	// No projects and nil metadata.
	f.Add("", "", "", "", false, 0)
	// Supported languages without an explicit test_command.
	f.Add("backend", "services/backend", "go", "", false, 1)
	f.Add("web", "web", "svelte", "", false, 1)
	f.Add("api", "api", "spring", "", false, 1)
	// Custom and empty languages must be skipped safely.
	f.Add("cli", ".", "rust", "", false, 1)
	f.Add("app", ".", "", "", false, 1)
	// Explicit test_command, including one with spaces. The empty-subshell
	// text is user-authored and valid here, so the generated-recipe invariant
	// must not reject it.
	f.Add("backend", "services/backend", "go", "go test -race ./...", true, 1)
	f.Add("web", "web", "svelte", "npm run test -- --coverage", true, 2)
	f.Add("app", ".", "go", "(cd x && )", true, 1)
	// Quoting hazards and a leading ./, which must render without an empty
	// command.
	f.Add("my \"quoted\" project", "path/with\"quote", "go", "", false, 1)
	f.Add("app", "./services/backend", "go", "", false, 1)

	f.Fuzz(func(t *testing.T, name, path, language, testCommand string, hasTestCommand bool, projectCount int) {
		count := clampFuzzProjectCount(projectCount)
		m := fuzzManifest(name, path, language, testCommand, hasTestCommand, count)
		input := fuzzInputs(name, path, language, testCommand, hasTestCommand, count)

		out := renderFuzzedMakefile(t, m)
		target := makefileTestTarget(t, out)

		// An explicit non-empty command is user-authored text: it is rendered
		// verbatim, and the generated-recipe invariants must not be applied to
		// bytes the user supplied.
		if hasTestCommand && testCommand != "" && testCommand != "go test ./..." {
			assertExplicitTestCommand(t, target, testCommand, input)
			return
		}

		assertGeneratedTestTarget(t, out, target, language, path, count, input)
	})
}

// clampFuzzProjectCount keeps the fuzz corpus small because the fuzzer may
// synthesize any int.
func clampFuzzProjectCount(projectCount int) int {
	if projectCount < 0 {
		return 0
	}
	if projectCount > 3 {
		return 3
	}
	return projectCount
}

// fuzzManifest builds the manifest for one fuzz input.
func fuzzManifest(name, path, language, testCommand string, hasTestCommand bool, count int) *types.Manifest {
	m := &types.Manifest{}
	if hasTestCommand {
		m.Metadata = map[string]string{"test_command": testCommand}
	}
	for i := 0; i < count; i++ {
		pName, pPath := name, path
		if i > 0 {
			// Keep entries distinct so a loop bug cannot be hidden by
			// duplicate projects.
			pName = name + "-" + strconv.Itoa(i)
			pPath = path + "/sub" + strconv.Itoa(i)
		}
		m.Projects = append(m.Projects, types.Project{
			Name:     pName,
			Path:     pPath,
			Language: language,
		})
	}
	return m
}

// assertGeneratedTestTarget applies the #148 invariants to the template-authored
// test target only. It is never called for an explicit test_command, whose text
// may legitimately look like an empty subshell.
func assertGeneratedTestTarget(t *testing.T, out, target, language, path string, count int, input string) {
	t.Helper()

	assertNoEmptyGeneratedRecipes(t, target, input)
	assertTestCommandsDefined(t, out)

	if count == 0 {
		assertNeutralNoProject(t, out, target, input)
		return
	}
	assertPerLanguageTestSelection(t, target, language, path, input)
}

// assertNoEmptyGeneratedRecipes checks the generated per-language branch for an
// empty subshell command or an empty language suffix.
func assertNoEmptyGeneratedRecipes(t *testing.T, target, input string) {
	t.Helper()

	if emptySubshellRecipe.MatchString(target) {
		t.Fatalf("rendered an empty subshell command for input %+v:\n%s", input, target)
	}
	if emptyLanguageTestRef.MatchString(target) {
		t.Fatalf("rendered $(MAKE_TEST_COMMAND_) with an empty language suffix for input %+v:\n%s", input, target)
	}
}

// assertNeutralNoProject checks that an empty manifest fails with actionable
// guidance rather than claiming there is nothing to test successfully.
func assertNeutralNoProject(t *testing.T, out, target, input string) {
	t.Helper()

	if strings.Contains(target, "go test ./...") {
		t.Fatalf("no-project Makefile emitted the legacy Go default for input %+v:\n%s", input, target)
	}
	if !strings.Contains(target, "No projects configured; set metadata.test_command to run root-level tests.") || !strings.Contains(target, "@exit 1") {
		t.Fatalf("no-project test target must fail with root-test guidance for input %+v:\n%s", input, target)
	}
}

// assertPerLanguageTestSelection checks that supported languages emit their
// per-language command while unsupported languages emit only a neutral skip.
func assertPerLanguageTestSelection(t *testing.T, target, language, path, input string) {
	t.Helper()

	switch language {
	case "go", "svelte", "spring":
		wantRecipe := fmt.Sprintf("(cd %s && $(MAKE_TEST_COMMAND_%s))", path, language)
		if !strings.Contains(target, wantRecipe) {
			t.Fatalf("test target missing %q for supported language %q for input %+v:\n%s", wantRecipe, language, input, target)
		}
	default:
		if makeTestCommandRef.MatchString(target) {
			t.Fatalf("test target emitted a per-language command for unsupported language %q for input %+v:\n%s", language, input, target)
		}
		if !strings.Contains(target, "No test command configured for") {
			t.Fatalf("test target missing the neutral skip line for unsupported language %q for input %+v:\n%s", language, input, target)
		}
	}
}

// assertExplicitTestCommand checks that a user-supplied test_command is rendered
// verbatim and suppresses the generated per-language loop. The generated-recipe
// invariants are deliberately skipped because the command is not generated.
func assertExplicitTestCommand(t *testing.T, target, testCommand, input string) {
	t.Helper()

	if !strings.Contains(target, testCommand) {
		t.Fatalf("test target ignored test_command %q for input %+v:\n%s", testCommand, input, target)
	}
	structural := strings.ReplaceAll(target, testCommand, "")
	if makeTestCommandRef.MatchString(structural) {
		t.Fatalf("test target emitted a per-language MAKE_TEST_COMMAND loop despite test_command %q for input %+v:\n%s", testCommand, input, target)
	}
}

// renderFuzzedMakefile renders makefile.tpl entirely in memory using the same
// embedded template and quote helper as the production renderer.
func renderFuzzedMakefile(t *testing.T, m *types.Manifest) string {
	t.Helper()

	tpl, err := template.New("makefile.tpl").Funcs(fuzzTemplateFuncs).ParseFS(templates, "makefile.tpl")
	if err != nil {
		t.Fatalf("parse embedded makefile.tpl: %v", err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, m); err != nil {
		t.Fatalf("execute embedded makefile.tpl for manifest %+v: %v", m, err)
	}
	return buf.String()
}

// fuzzInputs returns a compact, printable description of the failing input.
func fuzzInputs(name, path, language, testCommand string, hasTestCommand bool, count int) string {
	return fmt.Sprintf("name=%q path=%q language=%q test_command=%q has_test_command=%t projects=%d",
		name, path, language, testCommand, hasTestCommand, count)
}
