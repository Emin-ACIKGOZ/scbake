// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package templates

import (
	"embed"
	"fmt"
	"sort"
	"sync"

	"scbake/internal/types"
	cighub "scbake/pkg/templates/ci_github"
	"scbake/pkg/templates/community"
	"scbake/pkg/templates/compliance"
	"scbake/pkg/templates/devcontainer"
	"scbake/pkg/templates/editorconfig"
	"scbake/pkg/templates/git"
	golinter "scbake/pkg/templates/go_linter"
	"scbake/pkg/templates/makefile"
	mavenlinter "scbake/pkg/templates/maven_linter"
	sveltelinter "scbake/pkg/templates/svelte_linter"
)

// Handler is the interface all tooling template handlers must implement.
type Handler interface {
	// GetTasks takes a targetPath to be context-aware, and directories for overrides/cache
	GetTasks(targetPath string, templateDir string, registryCacheDir string) ([]types.Task, error)
}

// SchemaProvider is an optional interface a Handler can implement to declare
// the input variables it expects. scbake validates the manifest metadata
// against the schema before executing any tasks.
type SchemaProvider interface {
	Handler
	SchemaFS() embed.FS
	SchemaPath() string
}

var (
	handlersLock sync.RWMutex
	handlers     = map[string]Handler{
		"makefile":      &makefile.Handler{},
		"ci_github":     &cighub.Handler{},
		"editorconfig":  &editorconfig.Handler{},
		"go_linter":     &golinter.Handler{},
		"maven_linter":  &mavenlinter.Handler{},
		"svelte_linter": &sveltelinter.Handler{},
		"community":     &community.Handler{},
		"compliance":    &compliance.Handler{},
		"devcontainer":  &devcontainer.Handler{},
		"git":           &git.Handler{},
	}
)

// Register allows external packages (like tests) to inject custom handlers.
// This is essential for testing failure scenarios.
func Register(name string, h Handler) {
	handlersLock.Lock()
	defer handlersLock.Unlock()
	handlers[name] = h
}

// GetHandler returns the correct template handler for the given string.
func GetHandler(tmplName string) (Handler, error) {
	handlersLock.RLock()
	defer handlersLock.RUnlock()
	handler, ok := handlers[tmplName]
	if !ok {
		return nil, fmt.Errorf("unknown template: %s", tmplName)
	}
	return handler, nil
}

// GetSchema looks up the handler by name and, if it implements SchemaProvider,
// reads and parses its embedded schema.json. Returns nil, nil when the handler
// does not provide a schema (validation is skipped).
func GetSchema(tmplName string) (*embed.FS, string, error) {
	handlersLock.RLock()
	defer handlersLock.RUnlock()

	h, ok := handlers[tmplName]
	if !ok {
		return nil, "", fmt.Errorf("unknown template: %s", tmplName)
	}

	sp, ok := h.(SchemaProvider)
	if !ok {
		return nil, "", nil
	}

	fs := sp.SchemaFS()
	path := sp.SchemaPath()
	return &fs, path, nil
}

// ListTemplates returns the sorted names of all supported templates.
func ListTemplates() []string {
	handlersLock.RLock()
	defer handlersLock.RUnlock()
	keys := make([]string, 0, len(handlers))
	for k := range handlers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
