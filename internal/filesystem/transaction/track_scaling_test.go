// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// countTrackOnce returns the wall time to Track n distinct, not-yet-existing
// paths with a fresh manager. It is the measurement primitive for the scaling
// test and benchmark.
func countTrackOnce(t testing.TB, n int) time.Duration {
	t.Helper()
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	start := time.Now()
	for i := 0; i < n; i++ {
		p := filepath.Join(rootDir, "d", fmt.Sprintf("f%06d.txt", i))
		if err := tx.Track(p); err != nil {
			t.Fatalf("Track(%d) failed: %v", i, err)
		}
	}
	return time.Since(start)
}

// minTrackTime returns the fastest of runs measurements, which suppresses
// scheduler and GC noise that only ever makes a run slower.
func minTrackTime(t testing.TB, n, runs int) time.Duration {
	t.Helper()
	best := time.Duration(1<<63 - 1)
	for i := 0; i < runs; i++ {
		if d := countTrackOnce(t, n); d < best {
			best = d
		}
	}
	return best
}

// TestPerf_TrackScaling measures that Track is Θ(n) rather than Θ(n²) in the
// number of tracked paths (issue #57, SCB-061). alreadyTracked previously
// linearly scanned the created slice, so doubling n roughly quadrupled total
// track time; with an O(1) membership set it roughly doubles. The threshold
// sits well above the linear ratio (~2.0x) and below the quadratic ratio
// (~3.4x), so reverting alreadyTracked to a scan fails this test.
//
// It is a timing test, not a correctness test, so it is kept out of the default
// unit run: set SCBAKE_PERF=1 to enable it (it is also skipped in -short mode).
func TestPerf_TrackScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing-based scaling test in -short mode")
	}
	if os.Getenv("SCBAKE_PERF") == "" {
		t.Skip("set SCBAKE_PERF=1 to run the transaction scaling test")
	}

	const (
		n        = 8000
		runs     = 3
		maxRatio = 2.75
	)

	base := minTrackTime(t, n, runs)
	doubled := minTrackTime(t, 2*n, runs)
	if base <= 0 {
		t.Fatalf("base measurement was not positive: %v", base)
	}

	ratio := float64(doubled) / float64(base)
	t.Logf("n=%d: %v; n=%d: %v; ratio=%.2fx", n, base, 2*n, doubled, ratio)
	if ratio >= maxRatio {
		t.Errorf(
			"Track scaling ratio %.2fx per doubling at n=%d exceeds %.2fx; "+
				"alreadyTracked may have regressed to a linear scan",
			ratio, n, maxRatio,
		)
	}
}

// BenchmarkTrackScaling reports the cost of tracking n distinct paths. With
// O(1) membership ns/op grows linearly with n; with the old linear scan it
// grew quadratically.
func BenchmarkTrackScaling(b *testing.B) {
	for _, n := range []int{1000, 2000, 4000, 8000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				countTrackOnce(b, n)
			}
		})
	}
}
