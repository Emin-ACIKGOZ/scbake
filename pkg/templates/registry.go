// Package templates provides the registry and interface for tooling template handlers.
package templates

import (
	"fmt"
	"scbake/internal/types"
	cighub "scbake/pkg/templates/ci_github"
	devcontainer "scbake/pkg/templates/devcontainer"
	"scbake/pkg/templates/editorconfig"
	golinter "scbake/pkg/templates/go_linter"
	"scbake/pkg/templates/makefile"
	mavenlinter "scbake/pkg/templates/maven_linter"
	sveltelinter "scbake/pkg/templates/svelte_linter"
)

// Handler is the interface all tooling template handlers must implement.
type Handler interface {
	// GetTasks takes a targetPath to be context-aware
	GetTasks(targetPath string) ([]types.Task, error)
}

// Map of all available template handlers.
var handlers = map[string]Handler{
	"makefile":      &makefile.Handler{},
	"ci_github":     &cighub.Handler{},
	"editorconfig":  &editorconfig.Handler{},
	"go_linter":     &golinter.Handler{},
	"maven_linter":  &mavenlinter.Handler{},
	"svelte_linter": &sveltelinter.Handler{},
	"devcontainer":  &devcontainer.Handler{},
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
