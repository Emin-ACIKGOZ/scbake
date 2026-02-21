// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package lang provides the registry and interface for language-specific tasks.
package lang

import (
	"fmt"
	"sort"

	"scbake/internal/types"
	golang "scbake/pkg/lang/go"
	"scbake/pkg/lang/spring"
	"scbake/pkg/lang/svelte"
)

// Handler is the interface all language handlers must implement.
type Handler interface {
	// GetTasks takes a targetPath to be context-aware
	GetTasks(targetPath string) ([]types.Task, error)
}

// handlers Map of all available language handlers.
var handlers = map[string]Handler{
	"go":     &golang.Handler{},
	"svelte": &svelte.Handler{},
	"spring": &spring.Handler{},
}

// Register allows external packages or tests to inject custom language handlers.
func Register(name string, h Handler) {
	handlers[name] = h
}

// GetHandler returns the correct language handler for the given string.
func GetHandler(langName string) (Handler, error) {
	handler, ok := handlers[langName]
	if !ok {
		return nil, fmt.Errorf("unknown language: %s", langName)
	}
	return handler, nil
}

// ListLangs returns the sorted names of all supported languages.
func ListLangs() []string {
	keys := make([]string, 0, len(handlers))
	for k := range handlers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
