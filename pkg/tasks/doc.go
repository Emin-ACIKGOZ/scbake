// Package tasks defines the executable units of work used in a scaffolding plan.
//
// Every task runs under the transaction manager, so its changes are rolled back
// if a later task fails.
package tasks
