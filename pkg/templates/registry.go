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

// GetHandler returns the correct template handler for the given string.
func GetHandler(tmplName string) (Handler, error) {
	switch tmplName {
	case "makefile":
		return &makefile.Handler{}, nil
	default:
		return nil, fmt.Errorf("unknown template: %s", tmplName)
	}
}
