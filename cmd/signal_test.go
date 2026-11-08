// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"os"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/util/fileutil"
	"syscall"
	"testing"
)

func TestCommandExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		sig  os.Signal
		want int
	}{
		{"success", nil, nil, fileutil.ExitSuccess},
		{"failure", errors.New("boom"), nil, fileutil.ExitError},
		{"busy", transaction.ErrBusy, nil, fileutil.ExitBusy},
		{"wrapped busy", fmt.Errorf("apply: %w", transaction.ErrBusy), nil, fileutil.ExitBusy},
		{"SIGINT wins over success", nil, os.Interrupt, fileutil.ExitInterrupted},
		{"SIGTERM wins over success", nil, syscall.SIGTERM, fileutil.ExitTerminated},
		{"signal wins over failure", errors.New("boom"), os.Interrupt, fileutil.ExitInterrupted},
		{"signal wins over busy", transaction.ErrBusy, os.Interrupt, fileutil.ExitInterrupted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandExitCode(tt.err, tt.sig); got != tt.want {
				t.Errorf("commandExitCode(%v, %v) = %d, want %d", tt.err, tt.sig, got, tt.want)
			}
		})
	}
}

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
