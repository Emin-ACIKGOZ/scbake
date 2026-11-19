// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"strings"
	"testing"
)

func TestConflictStrategies(t *testing.T) {
	want := []string{"merge", "fail", "overwrite", "artifact", "keep-local"}
	got := ConflictStrategies()
	if len(got) != len(want) {
		t.Fatalf("ConflictStrategies() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ConflictStrategies()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestValidateConflictStrategyAcceptsDocumentedValues(t *testing.T) {
	for _, strategy := range ConflictStrategies() {
		if err := ValidateConflictStrategy(strategy); err != nil {
			t.Errorf("ValidateConflictStrategy(%q) = %v, want nil", strategy, err)
		}
	}
}

func TestValidateConflictStrategyRejectsUnknownValues(t *testing.T) {
	for _, strategy := range []string{"bogus", "Merge", "merg", "fail ", "overwrite,fail", ""} {
		t.Run(strategy, func(t *testing.T) {
			err := ValidateConflictStrategy(strategy)
			if err == nil {
				t.Fatalf("ValidateConflictStrategy(%q) = nil, want an invalid-value error", strategy)
			}
			if !strings.Contains(err.Error(), "invalid conflict strategy") {
				t.Errorf("error = %q, want it to reject the value as an invalid conflict strategy", err)
			}
			for _, valid := range ConflictStrategies() {
				if !strings.Contains(err.Error(), valid) {
					t.Errorf("error = %q, want it to name valid value %q", err, valid)
				}
			}
		})
	}
}
