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

// GetHandler returns the correct language handler for the given string.
func GetHandler(lang string) (Handler, error) {
	switch lang {
	case "go":
		return &golang.Handler{}, nil
	case "svelte":
		return &svelte.Handler{}, nil
	case "spring":
		return &spring.Handler{}, nil
	default:
		return nil, fmt.Errorf("unknown language: %s", lang)
	}
}
