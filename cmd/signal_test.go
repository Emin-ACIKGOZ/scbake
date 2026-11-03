// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"scbake/internal/util/fileutil"
	"syscall"
	"testing"
)

func TestExitCodeForSignal(t *testing.T) {
	tests := []struct {
		name string
		sig  os.Signal
		want int
	}{
		{"SIGINT", os.Interrupt, fileutil.ExitInterrupted},
		{"SIGTERM", syscall.SIGTERM, fileutil.ExitTerminated},
		{"unexpected", syscall.Signal(0), fileutil.ExitError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCodeForSignal(tt.sig); got != tt.want {
				t.Errorf("exitCodeForSignal(%v) = %d, want %d", tt.sig, got, tt.want)
			}
		})
	}
}
