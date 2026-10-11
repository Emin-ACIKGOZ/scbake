// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Package merge provides a conservative, line-oriented three-way merge used by
// `scbake update` to reconcile a generated file (the base) with local edits
// (ours) and a newer template render (theirs).
//
// The algorithm merges only edits whose base regions are disjoint. Any overlap,
// or any input that is not plain text, yields Conflict or NotText so the caller
// can fall back to an explicit policy (write an artifact). It never silently
// drops a change from either side, and it preserves trailing newlines.
package merge

import (
	"bytes"
	"slices"
	"strings"
)

// MaxLines bounds a text merge. Above this the merge reports NotText so the
// caller falls back to a policy instead of allocating an O(n*m) table.
const MaxLines = 20000

// Conflict marker labels.
const (
	OursLabel   = "local"
	TheirsLabel = "template"
)

// Status reports the outcome of a merge.
type Status int

const (
	// Clean means the inputs merged without conflict.
	Clean Status = iota

	// Conflict means both sides changed overlapping regions. Merged still holds
	// the result, annotated with conflict markers.
	Conflict

	// NotText means at least one input is binary or too large to merge safely.
	// Merged is nil; the caller must fall back to a policy.
	NotText
)

// Merge performs a three-way merge of ours and theirs, both derived from base.
//
// On NotText, merged is nil. On Conflict, merged contains standard conflict
// markers. On Clean, merged is the reconciled content. Inputs are treated as
// byte-exact; only '\n' is used as a line boundary.
func Merge(base, ours, theirs []byte) ([]byte, Status) {
	if out, ok := trivialMerge(base, ours, theirs); ok {
		return out, Clean
	}
	if isBinary(base) || isBinary(ours) || isBinary(theirs) {
		return nil, NotText
	}

	bl, ol, tl := splitLines(base), splitLines(ours), splitLines(theirs)
	if len(bl) > MaxLines || len(ol) > MaxLines || len(tl) > MaxLines {
		return nil, NotText
	}

	merged, conflicted := mergeRegions(bl, changedRegions(bl, ol), changedRegions(bl, tl))
	if conflicted {
		return merged, Conflict
	}
	return merged, Clean
}

// trivialMerge resolves the cases where one side did not change from base, so
// the other side wins verbatim without diffing.
func trivialMerge(base, ours, theirs []byte) ([]byte, bool) {
	switch {
	case bytes.Equal(base, ours):
		return theirs, true
	case bytes.Equal(base, theirs):
		return ours, true
	case bytes.Equal(ours, theirs):
		return ours, true
	default:
		return nil, false
	}
}

// isBinary reports whether data looks like a binary blob.
func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}

// splitLines splits data into line tokens, each including its trailing '\n'
// except possibly the last. Concatenating the tokens reproduces data exactly.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, string(data[start:i+1]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}

// region is a contiguous change in base coordinates: base[start:end] was
// replaced by lines in the derived file. start==end denotes an insertion.
type region struct {
	start int
	end   int
	lines []string
}

// changedRegions returns the regions of base that other changed, in order.
func changedRegions(base, other []string) []region {
	matches := lcsMatches(base, other)
	var regions []region
	bi, oi := 0, 0
	for _, m := range matches {
		if bi < m[0] || oi < m[1] {
			regions = append(regions, region{start: bi, end: m[0], lines: other[oi:m[1]]})
		}
		bi = m[0] + 1
		oi = m[1] + 1
	}
	if bi < len(base) || oi < len(other) {
		regions = append(regions, region{start: bi, end: len(base), lines: other[oi:]})
	}
	return regions
}

// lcsMatches returns the index pairs of a longest common subsequence of a and
// b. It uses an O(n*m) dynamic program; callers bound the input size.
func lcsMatches(a, b []string) [][2]int {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	return backtrackLCS(a, b, lcsTable(a, b))
}

// lcsTable returns dp where dp[i][j] is the length of the LCS of a[i:] and b[j:].
func lcsTable(a, b []string) [][]int {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	return dp
}

// backtrackLCS walks the DP table to recover the matched index pairs.
func backtrackLCS(a, b []string, dp [][]int) [][2]int {
	matches := make([][2]int, 0, dp[0][0])
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			matches = append(matches, [2]int{i, j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return matches
}

// side tags a region with the file it came from.
type side int

const (
	fromOurs side = iota
	fromTheirs
)

type taggedRegion struct {
	region
	side side
}

// mergeRegions merges the two region lists into the base. Regions are grouped
// into maximal clusters of overlapping (or touching) base spans; a cluster that
// contains edits from both sides is a conflict unless the two sides resolve to
// identical text.
func mergeRegions(base []string, ours, theirs []region) ([]byte, bool) {
	all := tagRegions(ours, theirs)

	var out []string
	conflicted := false
	pos := 0
	for i := 0; i < len(all); {
		j, groupEnd := groupEnd(all, i)
		groupStart := all[i].start

		out = append(out, base[pos:groupStart]...)
		oursGroup, theirsGroup := splitSides(all[i : j+1])
		resolved, conflict := resolveGroup(base, groupStart, groupEnd, oursGroup, theirsGroup)
		out = append(out, resolved...)
		conflicted = conflicted || conflict

		pos = groupEnd
		i = j + 1
	}
	out = append(out, base[pos:]...)
	return []byte(strings.Join(out, "")), conflicted
}

// tagRegions merges the two region lists into one sorted, side-tagged list.
func tagRegions(ours, theirs []region) []taggedRegion {
	all := make([]taggedRegion, 0, len(ours)+len(theirs))
	for _, r := range ours {
		all = append(all, taggedRegion{r, fromOurs})
	}
	for _, r := range theirs {
		all = append(all, taggedRegion{r, fromTheirs})
	}
	slices.SortStableFunc(all, func(x, y taggedRegion) int {
		if x.start != y.start {
			return x.start - y.start
		}
		return x.end - y.end
	})
	return all
}

// groupEnd returns the inclusive index of the last region in the cluster that
// starts at or before the cluster's end, and that cluster's end offset.
func groupEnd(all []taggedRegion, i int) (int, int) {
	end := all[i].end
	j := i
	for j+1 < len(all) && all[j+1].start <= end {
		j++
		end = max(end, all[j].end)
	}
	return j, end
}

// splitSides divides a cluster into the regions contributed by each side.
func splitSides(group []taggedRegion) (ours, theirs []region) {
	for _, r := range group {
		if r.side == fromOurs {
			ours = append(ours, r.region)
		} else {
			theirs = append(theirs, r.region)
		}
	}
	return ours, theirs
}

// resolveGroup applies a cluster to base, returning the replacement text and
// whether it is a conflict.
func resolveGroup(base []string, lo, hi int, ours, theirs []region) ([]string, bool) {
	switch {
	case len(ours) == 0:
		return applyRegions(base, lo, hi, theirs), false
	case len(theirs) == 0:
		return applyRegions(base, lo, hi, ours), false
	}

	oursText := applyRegions(base, lo, hi, ours)
	theirsText := applyRegions(base, lo, hi, theirs)
	if slices.Equal(oursText, theirsText) {
		return oursText, false
	}
	return appendConflict(nil, oursText, theirsText), true
}

// applyRegions returns base[lo:hi] with the given disjoint regions applied.
func applyRegions(base []string, lo, hi int, regions []region) []string {
	var out []string
	cursor := lo
	for _, r := range regions {
		if r.start > cursor {
			out = append(out, base[cursor:r.start]...)
		}
		out = append(out, r.lines...)
		cursor = r.end
	}
	if cursor < hi {
		out = append(out, base[cursor:hi]...)
	}
	return out
}

// appendConflict appends a conflict block, ensuring each side ends on its own
// line so the markers are never glued to content that lacked a trailing '\n'.
func appendConflict(out, ours, theirs []string) []string {
	out = append(out, "<<<<<<< "+OursLabel+"\n")
	out = appendSide(out, ours)
	out = append(out, "=======\n")
	out = appendSide(out, theirs)
	return append(out, ">>>>>>> "+TheirsLabel+"\n")
}

func appendSide(out, lines []string) []string {
	out = append(out, lines...)
	if len(lines) > 0 && !strings.HasSuffix(lines[len(lines)-1], "\n") {
		out = append(out, "\n")
	}
	return out
}
