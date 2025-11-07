package templates

import (
	"fmt"
	"scbake/internal/types"
	"scbake/pkg/templates/makefile"
)

// Handler is the interface all tooling template handlers must implement.
type Handler interface {
	// GetTasks now takes a targetPath to be context-aware
	GetTasks(targetPath string) ([]types.Task, error)
}

// Map of all available template handlers.
var handlers = map[string]Handler{
	"makefile": &makefile.Handler{},
}

// GetHandler returns the correct template handler for the given string.
func GetHandler(tmplName string) (Handler, error) {
	handler, ok := handlers[tmplName]
	if !ok {
		return nil, fmt.Errorf("unknown template: %s", tmplName)
	}
	return handler, nil
}

// ListTemplates returns the names of all supported templates.
func ListTemplates() []string {
	keys := make([]string, 0, len(handlers))
	for k := range handlers {
		keys = append(keys, k)
	}
	return keys
}
