// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build go1.18

package tasks

import (
	"bytes"
	"testing"
)

// FuzzHashContentEOL pins the normalization/hash contract:
//
//   - hashing never panics on arbitrary bytes;
//   - the hash is deterministic and idempotent under repeated normalization;
//   - NUL-containing content hashes byte-exactly (binary is not normalized);
//   - text CRLF content hashes identically to its LF form.
//
// Seeds cover empty, LF, CRLF, mixed, NUL binary, Unicode, and a larger text
// workload so the mutation engine starts from realistic shapes.
func FuzzHashContentEOL(f *testing.F) { //nolint:cyclop // multi-property fuzz oracle
	f.Add([]byte{})
	f.Add([]byte("alpha\nshared\nomega\n"))
	f.Add([]byte("alpha\r\nshared\r\nomega\r\n"))
	f.Add([]byte("alpha\r\nshared\nomega\r\n"))
	f.Add([]byte("\x00\xff\r\nbinary\x00\r\n"))
	f.Add([]byte("héllo\r\n世界\r\n"))
	f.Add(bytes.Repeat([]byte("line with some text\r\n"), 1<<13))

	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) > 1<<20 {
			t.Skip()
		}

		first := HashContent(content)
		if second := HashContent(content); second != first {
			t.Fatalf("hash is not deterministic: %q != %q", first, second)
		}
		if third := HashContent(append([]byte(nil), content...)); third != first {
			t.Fatalf("hash depends on the caller's buffer: %q != %q", first, third)
		}

		normalized := normalizeEOL(content)
		if again := normalizeEOL(normalized); !bytes.Equal(again, normalized) {
			t.Fatalf("normalizeEOL is not idempotent: %q -> %q -> %q", content, normalized, again)
		}
		if got := HashContent(normalized); got != first {
			t.Fatalf("hash changed under normalization: %q != %q", first, got)
		}

		lf := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
		if bytes.IndexByte(content, 0) >= 0 {
			if first != rawSHA256(content) {
				t.Fatalf("NUL content hash = %q, want byte-exact %q", first, rawSHA256(content))
			}
			if len(lf) != len(content) && first == HashContent(lf) {
				t.Fatalf("NUL content was incorrectly normalized: %q", content)
			}
			return
		}
		if got := HashContent(lf); got != first {
			t.Fatalf("text CRLF hash %q != LF hash %q for %q", first, got, content)
		}
	})
}
