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
	seq := NewPrioritySequence(100, 105)

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
	_, err := seq.Next()
	if err == nil {
		t.Error("expected error when exceeding max priority, got nil")
	}
}

// TestPrioritySequence_Concurrency simulates a high-load environment.
// It launches 100 goroutines at once to try and break the Mutex.
func TestPrioritySequence_Concurrency(t *testing.T) {
	seq := NewPrioritySequence(1000, 0) // 0 = Unlimited max
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
