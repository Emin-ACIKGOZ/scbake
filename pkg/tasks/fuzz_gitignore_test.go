// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build go1.18

package tasks

import (
	"strings"
	"testing"
)

// FuzzContentAlreadyPresentIdempotency checks that the pure line-aligned
// "already present" predicate agrees with the newline-padding rule used by
// appendFileContent. For any non-whitespace content that is reported absent,
// simulating exactly one append must make it present, so a second append can
// never be needed.
func FuzzContentAlreadyPresentIdempotency(f *testing.F) {
	// Empty file and empty content.
	f.Add("", "")
	// Plain .gitignore content.
	f.Add("vendor/\n*.log\n", ".scbake/\n")
	// Existing file without a trailing newline.
	f.Add("vendor/", "*.log")
	// Content already present (single line, multiline).
	f.Add(".scbake/\n", ".scbake/\n")
	f.Add("a\nb\nc\n", "b\nc")
	// Content absent despite sharing lines.
	f.Add("a\nb\nc\n", "b\nd")
	// CRLF and comments.
	f.Add("# .scbake/\r\nvendor/\r\n", ".scbake/\n")
	f.Add("# .scbake/\r\n", ".scbake/\n")
	// Leading/trailing blank lines in content.
	f.Add("x\n", "\nscbake/\n\n")

	f.Fuzz(func(t *testing.T, existing, content string) {
		present := contentAlreadyPresent(existing, content)

		if strings.TrimSpace(content) == "" {
			if !present {
				t.Fatalf("whitespace-only content must be treated as present: existing=%q content=%q", existing, content)
			}
			return
		}

		if present {
			return
		}

		// Simulate the append performed by appendFileContent: pad with a
		// single newline first when a non-empty file does not end with one.
		appended := existing
		if len(appended) > 0 && appended[len(appended)-1] != '\n' {
			appended += "\n"
		}
		appended += content

		if !contentAlreadyPresent(appended, content) {
			t.Fatalf("append is not idempotent: existing=%q content=%q appended=%q", existing, content, appended)
		}
	})
}

// FuzzGitignoreIgnoresInternalState checks that only the four supported
// standalone .gitignore spellings are recognized. Surrounding text, comments,
// path prefixes, and arbitrary bytes must not produce a false positive, while
// trailing spaces, tabs, and CR must not hide a real entry. Leading whitespace
// is part of the Git pattern and must not be trimmed.
func FuzzGitignoreIgnoresInternalState(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("vendor/\n*.log\nnode_modules/\n"))
	f.Add([]byte(".scbake")) // no trailing newline
	f.Add([]byte(".scbake/"))
	f.Add([]byte("/.scbake/"))
	f.Add([]byte("# .scbake/"))
	f.Add([]byte("foo/.scbake/"))
	f.Add([]byte(".scbake/\r\n"))
	f.Add([]byte("vendor/\r\n.scbake\r\n*.log\r\n"))
	f.Add([]byte(".scbake/ \t\r\n")) // trailing whitespace only
	f.Add([]byte("  .scbake/\n"))    // leading whitespace is part of the pattern
	f.Add([]byte("\x00\xff.scbake/\x00\n"))
	f.Add([]byte(".scbake/\nfoo\n"))

	spellings := []string{".scbake", ".scbake/", "/.scbake", "/.scbake/"}

	f.Fuzz(func(t *testing.T, input []byte) {
		got := gitignoreIgnoresInternalState(input)

		// Independent specification: true iff at least one line with only
		// trailing spaces/tabs/CR removed is exactly one of the supported
		// spellings.
		want := false
		var kept []string
		for _, line := range strings.Split(string(input), "\n") {
			trimmed := strings.TrimRight(line, " \t\r")
			isSpelling := false
			for _, spelling := range spellings {
				if trimmed == spelling {
					isSpelling = true
					break
				}
			}
			if isSpelling {
				want = true
				continue
			}
			kept = append(kept, line)
		}
		if got != want {
			t.Fatalf("gitignoreIgnoresInternalState(%q) = %v, want %v", input, got, want)
		}

		// Nothing other than an exact spelling line may match: removing every
		// spelling line must leave a non-matching input.
		if got && gitignoreIgnoresInternalState([]byte(strings.Join(kept, "\n"))) {
			t.Fatalf("non-spelling lines matched: input=%q remaining=%q", input, strings.Join(kept, "\n"))
		}

		// CRLF normalization must not change the result.
		lf := strings.ReplaceAll(string(input), "\r\n", "\n")
		if lf != string(input) && gitignoreIgnoresInternalState([]byte(lf)) != got {
			t.Fatalf("CRLF normalization changed result: input=%q lf=%q", input, lf)
		}
	})
}
