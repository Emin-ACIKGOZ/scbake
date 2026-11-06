// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"scbake/internal/util/fileutil"
)

// signalContext returns a context that is canceled by SIGINT or SIGTERM, a
// function that releases the signal handler, and a function reporting which
// signal caused the cancellation (nil when the command finished normally).
//
// The signal identity is needed to return the conventional 128+signal exit
// status. os/signal.NotifyContext cancels its context but does not expose the
// signal, so this uses Notify directly.
func signalContext(parent context.Context) (context.Context, func(), func() os.Signal) {
	ctx, cancel := context.WithCancel(parent)
	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var (
		mu       sync.Mutex
		received os.Signal
	)
	done := make(chan struct{})
	stopCh := make(chan struct{})
	var stopOnce sync.Once

	go func() {
		defer close(done)
		select {
		case sig := <-sigCh:
			mu.Lock()
			received = sig
			mu.Unlock()
			cancel()
		case <-ctx.Done():
			return
		}

		// The first signal starts graceful rollback. A second signal must not
		// be swallowed if that rollback is stuck, so exit immediately with the
		// same 128+signal status.
		select {
		case sig := <-sigCh:
			os.Exit(exitCodeForSignal(sig))
		case <-stopCh:
		}
	}()

	stop := func() {
		stopOnce.Do(func() {
			signal.Stop(sigCh)
			close(stopCh)
			cancel()
		})
		// Wait for the relay goroutine so received is final before the caller
		// selects an exit status.
		<-done
	}

	receivedSignal := func() os.Signal {
		mu.Lock()
		defer mu.Unlock()
		return received
	}

	return ctx, stop, receivedSignal
}

// exitCodeForSignal maps a terminating signal to its conventional shell exit
// status: 128 + the signal number. It returns the generic failure status for
// an unexpected signal so the process never exits 0 after an interruption.
func exitCodeForSignal(sig os.Signal) int {
	switch sig {
	case os.Interrupt:
		return fileutil.ExitInterrupted
	case syscall.SIGTERM:
		return fileutil.ExitTerminated
	default:
		return fileutil.ExitError
	}
}

// commandExitCode maps a finished command and any received signal to a process
// exit status. A received signal always wins, even when the command completed
// successfully, so an interruption can never result in exit 0.
func commandExitCode(err error, sig os.Signal) int {
	if sig != nil {
		return exitCodeForSignal(sig)
	}
	if err != nil {
		return fileutil.ExitError
	}
	return fileutil.ExitSuccess
}
