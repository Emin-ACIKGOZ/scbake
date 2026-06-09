// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package lang

import (
	"embed"
	"fmt"
	"sort"
	"sync"

	"scbake/internal/types"
	golang "scbake/pkg/lang/go"
	"scbake/pkg/lang/spring"
	"scbake/pkg/lang/svelte"
)

// SchemaProvider is an optional interface a language Handler can implement to
// declare the input variables it expects. scbake validates the manifest
// metadata against the schema before executing any tasks.
type SchemaProvider interface {
	Handler
	SchemaFS() embed.FS
	SchemaPath() string
}

// Handler is the interface all language handlers must implement.
type Handler interface {
	// GetTasks takes a targetPath to be context-aware, and optional directories for overrides/cache
	GetTasks(targetPath string, templateDir string, registryCacheDir string) ([]types.Task, error)
}

var (
	handlersLock sync.RWMutex
	handlers     = map[string]Handler{
		"go":     &golang.Handler{},
		"svelte": &svelte.Handler{},
		"spring": &spring.Handler{},
	}
)

// Register allows external packages or tests to inject custom language handlers.
func Register(name string, h Handler) {
	handlersLock.Lock()
	defer handlersLock.Unlock()
	handlers[name] = h
}

// GetHandler returns the correct language handler for the given string.
func GetHandler(langName string) (Handler, error) {
	handlersLock.RLock()
	defer handlersLock.RUnlock()
	handler, ok := handlers[langName]
	if !ok {
		return nil, fmt.Errorf("unknown language: %s", langName)
	}
	return handler, nil
}

// GetSchema looks up the handler by name and, if it implements SchemaProvider,
// reads and parses its embedded schema.json. Returns nil, nil when the handler
// does not provide a schema (validation is skipped).
func GetSchema(langName string) (*embed.FS, string, error) {
	handlersLock.RLock()
	defer handlersLock.RUnlock()

	h, ok := handlers[langName]
	if !ok {
		return nil, "", fmt.Errorf("unknown language: %s", langName)
	}

	sp, ok := h.(SchemaProvider)
	if !ok {
		return nil, "", nil
	}

	fs := sp.SchemaFS()
	path := sp.SchemaPath()
	return &fs, path, nil
}

// ListLangs returns the sorted names of all supported languages.
func ListLangs() []string {
	handlersLock.RLock()
	defer handlersLock.RUnlock()
	keys := make([]string, 0, len(handlers))
	for k := range handlers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
