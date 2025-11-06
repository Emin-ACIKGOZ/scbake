package lang

import (
	golang "scbake/pkg/lang/go" // aliasing to avoid collision

	"fmt"
	"scbake/internal/types"
)

// Handler is the interface all language handlers must implement.
type Handler interface {
	// GetTasks now takes a targetPath to be context-aware
	GetTasks(targetPath string) ([]types.Task, error)
}

// GetHandler returns the correct language handler for the given string.
func GetHandler(lang string) (Handler, error) {
	// ... switch case unchanged ...
	switch lang {
	case "go":
		return &golang.Handler{}, nil
	default:
		return nil, fmt.Errorf("unknown language: %s", lang)
	}
}
