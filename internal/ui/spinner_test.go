// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSpinnerReporterSequentialTaskLifecycle is a regression test for #255: a
// sequential TaskStart/TaskEnd loop must not race the lifecycle fields against
// the animation goroutine. Run this test with -race to exercise the detector.
func TestSpinnerReporterSequentialTaskLifecycle(_ *testing.T) {
	r := NewSpinnerReporter(50)
	for i := 0; i < 50; i++ {
		r.TaskStart("step", i+1, 50)
		r.TaskEnd(nil)
	}
}

// TestSpinnerReporterSupersedeJoinsPrevious covers a start without a prior
// end: the superseded renderer must be joined before TaskEnd returns.
func TestSpinnerReporterSupersedeJoinsPrevious(t *testing.T) { //nolint:cyclop // pipe reader/join assertions
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = oldStdout
		_ = writer.Close()
		_ = reader.Close()
	})

	chunks := make(chan []byte, 16)
	go func() {
		defer close(chunks)
		chunk := make([]byte, 256)
		for {
			n, readErr := reader.Read(chunk)
			if n > 0 {
				out := make([]byte, n)
				copy(out, chunk[:n])
				chunks <- out
			}
			if readErr != nil {
				return
			}
		}
	}()

	r := NewSpinnerReporter(2)
	r.TaskStart("first", 1, 2)
	r.TaskStart("second", 2, 2)
	r.TaskEnd(nil)

	const finalLine = "[2/2] ✅ second\n"
	var got strings.Builder
	deadline := time.After(2 * time.Second)
	for !strings.HasSuffix(got.String(), finalLine) {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("stdout closed before the final indicator; output = %q", got.String())
			}
			got.Write(chunk)
		case <-deadline:
			t.Fatalf("timed out waiting for the final indicator; output = %q", got.String())
		}
	}
	select {
	case chunk, ok := <-chunks:
		if ok {
			got.Write(chunk)
		}
		t.Fatalf("a superseded spinner wrote after TaskEnd returned; output = %q", got.String())
	case <-time.After(2 * spinnerDelay):
	}
}

// TestSpinnerReporterTaskEndIsIdempotent is a regression test for #255: ending a
// task twice must be a no-op instead of panicking on a closed channel.
func TestSpinnerReporterTaskEndIsIdempotent(_ *testing.T) {
	r := NewSpinnerReporter(1)

	r.TaskStart("step", 1, 1)
	r.TaskEnd(nil)

	// A duplicated completion, including one carrying an error, is a no-op.
	r.TaskEnd(errors.New("duplicate completion"))

	// The reporter must remain usable afterwards.
	r.TaskStart("next", 1, 1)
	r.TaskEnd(errors.New("boom"))
	r.TaskEnd(nil)
}

// TestSpinnerReporterTaskEndBeforeTaskStart documents that ending a task that
// never started is a no-op rather than a nil-channel panic.
func TestSpinnerReporterTaskEndBeforeTaskStart(_ *testing.T) {
	r := NewSpinnerReporter(1)
	r.TaskEnd(nil)
}

// TestSpinnerReporterTaskEndJoinsSpinner is a regression test for #255: once
// TaskEnd returns, the animation goroutine must have exited and no spinner
// output may arrive afterwards.
func TestSpinnerReporterTaskEndJoinsSpinner(t *testing.T) { //nolint:cyclop // pipe reader/join assertions
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}

	oldStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = oldStdout
		_ = writer.Close()
		_ = reader.Close()
	})

	chunks := make(chan []byte, 16)
	go func() {
		defer close(chunks)
		chunk := make([]byte, 256)
		for {
			n, readErr := reader.Read(chunk)
			if n > 0 {
				out := make([]byte, n)
				copy(out, chunk[:n])
				chunks <- out
			}
			if readErr != nil {
				return
			}
		}
	}()

	r := NewSpinnerReporter(1)
	r.TaskStart("step", 1, 1)
	r.TaskEnd(nil)

	const finalLine = "[1/1] ✅ step\n"
	var got strings.Builder
	// Wait until the final indicator has reached the pipe. A spinner tick that
	// happened before TaskEnd may legitimately precede it.
	deadline := time.After(2 * time.Second)
	for !strings.HasSuffix(got.String(), finalLine) {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("stdout closed before the final indicator; output = %q", got.String())
			}
			got.Write(chunk)
		case <-deadline:
			t.Fatalf("timed out waiting for the final indicator; output = %q", got.String())
		}
	}

	// TaskEnd joined the animation goroutine, so a short quiet window must
	// observe no further output. The window only checks for the absence of a
	// write; it does not wait for one to happen.
	select {
	case chunk, ok := <-chunks:
		if ok {
			got.Write(chunk)
		}
		t.Fatalf("spinner wrote after TaskEnd returned; output = %q", got.String())
	case <-time.After(2 * spinnerDelay):
	}
}
