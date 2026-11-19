// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"scbake/internal/types"
	"strings"
	"testing"
)

// acceptedConflictStrategies is spelled out so the regression test still
// compiles against the pre-fix baseline and fails on behavior, not API.
var acceptedConflictStrategies = []string{
	types.ConflictMerge,
	types.ConflictFail,
	types.ConflictOverwrite,
	types.ConflictArtifact,
	types.ConflictKeepLocal,
}

// TestHandleConflictValidStrategies pins the behavior of every accepted
// conflict strategy, including the zero-value default that direct library
// callers rely on.
func TestHandleConflictValidStrategies(t *testing.T) {
	const (
		absPath       = "main.go"
		outputRelPath = "main.go"
	)

	cases := []struct {
		name     string
		strategy string
		wantPath string
		wantErr  string
	}{
		{
			name:     "merge falls back to artifact",
			strategy: types.ConflictMerge,
			wantPath: absPath + ".scbake-new",
		},
		{
			name:     "fail aborts",
			strategy: types.ConflictFail,
			wantPath: "",
			wantErr:  "manual modifications",
		},
		{
			name:     "zero value keeps the fail default",
			strategy: "",
			wantPath: "",
			wantErr:  "manual modifications",
		},
		{
			name:     "overwrite returns the canonical path",
			strategy: types.ConflictOverwrite,
			wantPath: absPath,
		},
		{
			name:     "artifact returns the side path",
			strategy: types.ConflictArtifact,
			wantPath: absPath + ".scbake-new",
		},
		{
			name:     "keep-local skips the write",
			strategy: types.ConflictKeepLocal,
			wantPath: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotPath string
				err     error
			)
			captureStderr(t, func() {
				gotPath, err = handleConflict(absPath, outputRelPath, tc.strategy)
			})

			if gotPath != tc.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tc.wantPath)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("handleConflict(%q) = %v, want nil", tc.strategy, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("handleConflict(%q) = nil, want %q error", tc.strategy, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
			}
			if strings.Contains(err.Error(), "invalid conflict strategy") {
				t.Errorf("error = %q, want the strategy to be accepted", err)
			}
		})
	}
}

// TestHandleConflictRejectsUnknownStrategy is the regression for #261: an
// unknown strategy used to hit the fail fallthrough, so a typo was
// indistinguishable from a real drift failure. It must now fail closed and name
// the accepted values.
func TestHandleConflictRejectsUnknownStrategy(t *testing.T) {
	const (
		absPath       = "main.go"
		outputRelPath = "main.go"
	)

	for _, strategy := range []string{"bogus", "Merge", "merg", "fail "} {
		t.Run(strategy, func(t *testing.T) {
			var (
				gotPath string
				err     error
			)
			captureStderr(t, func() {
				gotPath, err = handleConflict(absPath, outputRelPath, strategy)
			})

			if gotPath != "" {
				t.Errorf("path = %q, want no write path", gotPath)
			}
			if err == nil {
				t.Fatalf("handleConflict(%q) = nil, want an invalid-strategy error", strategy)
			}
			if !strings.Contains(err.Error(), "invalid conflict strategy") {
				t.Errorf("error = %q, want it to reject the unknown strategy", err)
			}
			for _, valid := range acceptedConflictStrategies {
				if !strings.Contains(err.Error(), valid) {
					t.Errorf("error = %q, want it to name valid value %q", err, valid)
				}
			}
		})
	}
}
