package lang

import (
	"fmt"
	golang "scbake/pkg/lang/go" // aliasing to avoid collision

	"scbake/internal/types"
)

// Handler is the interface all language handlers must implement.
type Handler interface {
	GetTasks() ([]types.Task, error)
}

// GetHandler returns the correct language handler for the given string.
func GetHandler(lang string) (Handler, error) {
	switch lang {
	case "go":
		return &golang.Handler{}, nil
	// case "python":
	// 	return &python.Handler{}, nil
	default:
		return nil, fmt.Errorf("unknown language: %s", lang)
	}
}
