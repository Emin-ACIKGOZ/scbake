// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package types

import (
	"sync"
	"testing"
)

// TestPrioritySequence_Sequential verifies basic counting logic.
// It ensures that a sequence counts up correctly (100, 101, 102...)
// and errors out if it hits the maximum.
func TestPrioritySequence_Sequential(t *testing.T) {
	// Start at 100, max is 105
	seq, err := NewPrioritySequence(100, 105)
	if err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}

	// We expect 6 valid numbers: 100, 101, 102, 103, 104, 105
	for i := 0; i < 6; i++ {
		p, err := seq.Next()
		if err != nil {
			t.Fatalf("unexpected error at index %d: %v", i, err)
		}
		expected := Priority(100 + i)
		if p != expected {
			t.Errorf("expected priority %d, got %d", expected, p)
		}
	}

	// The 7th call should fail (106 > 105)
	_, err = seq.Next()
	if err == nil {
		t.Error("expected error when exceeding max priority, got nil")
	}
}

// TestPrioritySequence_Concurrency simulates a high-load environment.
// It launches 100 goroutines at once to try and break the Mutex.
func TestPrioritySequence_Concurrency(t *testing.T) {
	seq, err := NewPrioritySequence(1000, 0) // 0 = Unlimited max
	if err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	count := 100
	var wg sync.WaitGroup

	// Channel to collect results from all goroutines
	generated := make(chan Priority, count)

	// Launch 100 goroutines simultaneously
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := seq.Next()
			if err != nil {
				t.Errorf("concurrent next failed: %v", err)
			}
			generated <- p
		}()
	}

	wg.Wait()
	close(generated)

	// Verify we have 100 UNIQUE numbers
	uniqueMap := make(map[Priority]bool)
	for p := range generated {
		if uniqueMap[p] {
			t.Errorf("duplicate priority generated: %d. Mutex failed!", p)
		}
		uniqueMap[p] = true
	}

	if len(uniqueMap) != count {
		t.Errorf("expected %d unique priorities, got %d", count, len(uniqueMap))
	}
}

// TestPrioritySequence_InvalidBand verifies that creating a sequence with invalid parameters returns error.
func TestPrioritySequence_InvalidBand(t *testing.T) {
	// base > max should return error
	_, err := NewPrioritySequence(200, 100)
	if err == nil {
		t.Error("expected error when base > max, got nil")
	}

	// base == max should be valid
	seq, err := NewPrioritySequence(100, 100)
	if err != nil {
		t.Fatalf("base == max should be valid, got error: %v", err)
	}

	// Should be able to get exactly one priority
	p, err := seq.Next()
	if err != nil {
		t.Fatalf("failed to get priority: %v", err)
	}
	if p != 100 {
		t.Errorf("expected priority 100, got %d", p)
	}

	// Next call should fail since we've exceeded max
	_, err = seq.Next()
	if err == nil {
		t.Error("expected error after exceeding max, got nil")
	}
}
