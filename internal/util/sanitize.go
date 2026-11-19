// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	hexDigits = "0123456789abcdef"

	// hexByteWidth and hexRuneWidth are the number of hex digits in the \xNN
	// and \uNNNN escapes; bitsPerHexDigit positions each digit.
	hexByteWidth    = 2
	hexRuneWidth    = 4
	bitsPerHexDigit = 4
	hexDigitMask    = 0xf

	byteEscape = `\x`
	runeEscape = `\u`
)

// Byte-range controls, escaped as \xNN.
const (
	controlLast = 0x1f
	del         = 0x7f
	c1First     = 0x80
	c1Last      = 0x9f
)

// Unicode line and bidi format controls, escaped as \uNNNN.
const (
	arabicLetterMark  = '\u061c'
	leftToRightMark   = '\u200e'
	rightToLeftMark   = '\u200f'
	lineSeparator     = '\u2028'
	bidiEmbeddingLast = '\u202e'
	bidiIsolateFirst  = '\u2066'
	bidiIsolateLast   = '\u2069'
)

// SanitizeModuleName creates a valid Go module name from a path.
// It takes the base name, lowercases it, and replaces spaces with hyphens.
// It returns an error if the resulting name starts with a dash, which
// `go mod init` would parse as a flag.
func SanitizeModuleName(path string) (string, error) {
	// Get the base name (e.g., "my cool app")
	name := filepath.Base(path)
	if name == "." || name == "/" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		name = filepath.Base(abs)
	}

	// Sanitize the name
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")

	// Reject rather than normalize, so the name is not silently altered.
	if strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("invalid module name %q: leading dash", name)
	}

	return name, nil
}

// Sanitize renders terminal control characters in s as literal backslash
// escapes so untrusted manifest values cannot move the cursor, recolor the
// terminal, emit OSC sequences, create forged lines, or inject lines into a CI
// log.
//
// C0 controls (0x00-0x1f, including ESC, BEL, CR, LF, and TAB), DEL (0x7f), and
// C1 controls (0x80-0x9f) become \xNN. Invalid UTF-8 bytes, including lone C1
// bytes and truncated multibyte sequences, become \xNN as well, so they can
// never reach a terminal raw. Unicode line and bidi format controls (U+061C,
// U+200E-U+200F, U+2028-U+202E, and U+2066-U+2069) become \uNNNN.
//
// Everything else, including ordinary Unicode and existing literal backslash
// sequences, is returned unchanged.
//
// This function always renders byte controls as \xNN. A composed message that
// also quotes an untrusted value with %q may show Go's own short escapes (for
// example \a for BEL) for those same bytes, so the representation is not
// uniform across the whole message; only this function's output is.
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	changed := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		invalid := r == utf8.RuneError && size == 1
		prefix, width, ok := escapeFor(r, invalid)
		if !ok {
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		if invalid {
			// Escape the offending byte rather than the replacement rune.
			r = rune(s[i])
		}
		writeHexEscape(&b, prefix, r, width)
		changed = true
		i += size
	}
	if !changed {
		return s
	}
	return b.String()
}

// sanitizedError wraps an error with a terminal-safe rendering while keeping
// the original error available to errors.Is and errors.As.
type sanitizedError struct {
	err error
}

// Error returns the sanitized display text of the wrapped error.
func (e *sanitizedError) Error() string { return Sanitize(e.err.Error()) }

// Unwrap exposes the original error so error-chain inspection still works.
func (e *sanitizedError) Unwrap() error { return e.err }

// SanitizeError returns an error whose Error text is safe to display and whose
// Unwrap returns err. Use it with %w so wrapped failures stay inspectable with
// errors.Is and errors.As while their displayed text escapes terminal controls.
func SanitizeError(err error) error {
	if err == nil {
		return nil
	}
	return &sanitizedError{err: err}
}

// escapeFor reports how to escape r for terminal display: the escape prefix,
// the number of hex digits, and whether r needs escaping at all. invalid marks
// a byte that is not valid UTF-8.
func escapeFor(r rune, invalid bool) (prefix string, width int, ok bool) {
	switch {
	case invalid:
		return byteEscape, hexByteWidth, true
	case isByteControl(r):
		return byteEscape, hexByteWidth, true
	case isFormatControl(r):
		return runeEscape, hexRuneWidth, true
	default:
		return "", 0, false
	}
}

// isByteControl reports C0 controls, DEL, and C1 controls.
func isByteControl(r rune) bool {
	return r <= controlLast || r == del || (r >= c1First && r <= c1Last)
}

// isFormatControl reports Unicode line and bidi format controls.
func isFormatControl(r rune) bool {
	return r == arabicLetterMark ||
		(r >= leftToRightMark && r <= rightToLeftMark) ||
		(r >= lineSeparator && r <= bidiEmbeddingLast) ||
		(r >= bidiIsolateFirst && r <= bidiIsolateLast)
}

// writeHexEscape appends escape followed by width lowercase hex digits for r.
func writeHexEscape(b *strings.Builder, escape string, r rune, width int) {
	b.WriteString(escape)
	for shift := (width - 1) * bitsPerHexDigit; shift >= 0; shift -= bitsPerHexDigit {
		b.WriteByte(hexDigits[(r>>uint(shift))&hexDigitMask])
	}
}
