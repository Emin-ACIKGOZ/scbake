// Package lang provides the registry and interface for language-specific tasks.
package lang

import (
	"fmt"
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

// Map of all available language handlers.
var handlers = map[string]Handler{
	"go":     &golang.Handler{},
	"svelte": &svelte.Handler{},
	"spring": &spring.Handler{},
}

// GetHandler returns the correct language handler for the given string.
func GetHandler(lang string) (Handler, error) {
	handler, ok := handlers[lang]
	if !ok {
		return nil, fmt.Errorf("unknown language: %s", lang)
	}
	return handler, nil
}

// ListLangs returns the names of all supported languages.
func ListLangs() []string {
	keys := make([]string, 0, len(handlers))
	for k := range handlers {
		keys = append(keys, k)
	}
	return keys
}
