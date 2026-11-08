// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestRecoverCommandHonorsCanceledContext verifies that `recover` returns the
// context error instead of reporting that no transaction was found after a
// SIGINT/SIGTERM canceled the command.
func TestRecoverCommandHonorsCanceledContext(t *testing.T) {
	resetFlags()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rootCmd.SetContext(ctx)
	t.Cleanup(func() { rootCmd.SetContext(context.Background()) })

	var err error
	output := captureStdout(t, func() {
		err = executeCLI("recover", t.TempDir())
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("recover error = %v, want context.Canceled", err)
	}
	if strings.Contains(output, "No interrupted transaction found") {
		t.Errorf("recover reported no transaction after cancellation: %q", output)
	}
}
