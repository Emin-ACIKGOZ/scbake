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

// fuzzTemplateFuncs mirrors the helpers the production renderer installs in
// pkg/tasks. The Makefile template currently only calls quote, and the
// implementation is kept byte-for-byte equivalent to tasks.templateFuncs.
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
//  3. a present, non-empty test_command is used verbatim and suppresses the
//     per-language test loop;
//  4. no projects and no test_command yields the neutral message and never a
//     Go test default;
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
	// Explicit test_command, including one with spaces.
	f.Add("backend", "services/backend", "go", "go test -race ./...", true, 1)
	f.Add("web", "web", "svelte", "npm run test -- --coverage", true, 2)
	// Quoting hazards and a leading ./, which must render without an empty
	// command.
	f.Add("my \"quoted\" project", "path/with\"quote", "go", "", false, 1)
	f.Add("app", "./services/backend", "go", "", false, 1)

	f.Fuzz(func(t *testing.T, name, path, language, testCommand string, hasTestCommand bool, projectCount int) {
		// Keep the corpus small: the fuzzer may synthesize any int.
		count := projectCount
		if count < 0 {
			count = 0
		}
		if count > 3 {
			count = 3
		}

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

		out := renderFuzzedMakefile(t, m)
		target := makefileTestTarget(t, out)
		assertTestCommandsDefined(t, out)

		// Invariant 2: no empty shell command or empty language suffix.
		if emptySubshellRecipe.MatchString(out) {
			t.Fatalf("rendered an empty subshell command for input %+v:\n%s", fuzzInputs(name, path, language, testCommand, hasTestCommand, count), out)
		}
		if emptyLanguageTestRef.MatchString(out) {
			t.Fatalf("rendered $(MAKE_TEST_COMMAND_) with an empty language suffix for input %+v:\n%s", fuzzInputs(name, path, language, testCommand, hasTestCommand, count), out)
		}

		hasCommand := hasTestCommand && testCommand != ""

		// Invariant 3: an explicit command wins and the per-language loop is
		// absent. Remove the command text before looking for loop markers so
		// data that merely looks like a marker does not create a false alarm.
		if hasCommand {
			if !strings.Contains(target, testCommand) {
				t.Fatalf("test target ignored test_command %q for input %+v:\n%s", testCommand, fuzzInputs(name, path, language, testCommand, hasTestCommand, count), target)
			}
			structural := strings.ReplaceAll(target, testCommand, "")
			if makeTestCommandRef.MatchString(structural) {
				t.Fatalf("test target emitted a per-language MAKE_TEST_COMMAND loop despite test_command %q:\n%s", testCommand, target)
			}
		}

		// Invariant 4: no projects + no command = neutral, non-Go test target.
		if count == 0 && !hasCommand {
			if strings.Contains(out, "go test ./...") {
				t.Fatalf("no-project Makefile emitted the Go test default:\n%s", out)
			}
			if !strings.Contains(target, "No projects configured; nothing to test.") {
				t.Fatalf("no-project test target is not the neutral message:\n%s", target)
			}
		}

		// Invariant 5: per-language selection by exact supported language.
		if count > 0 && !hasCommand {
			switch language {
			case "go", "svelte", "spring":
				wantRecipe := fmt.Sprintf("(cd %s && $(MAKE_TEST_COMMAND_%s))", path, language)
				if !strings.Contains(target, wantRecipe) {
					t.Fatalf("test target missing %q for supported language %q:\n%s", wantRecipe, language, target)
				}
			default:
				if makeTestCommandRef.MatchString(target) {
					t.Fatalf("test target emitted a per-language command for unsupported language %q:\n%s", language, target)
				}
				if !strings.Contains(target, "No test command configured for") {
					t.Fatalf("test target missing the neutral skip line for unsupported language %q:\n%s", language, target)
				}
			}
		}
	})
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
