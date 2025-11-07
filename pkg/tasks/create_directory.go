package tasks

import (
	"fmt"
	"os"
	"scbake/internal/types"
)

// CreateDirTask ensures a directory exists.
type CreateDirTask struct {
	Path     string
	Desc     string
	TaskPrio int
}

func (t *CreateDirTask) Execute(tc types.TaskContext) error {
	if err := os.MkdirAll(t.Path, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", t.Path, err)
	}
	return nil
}

func (t *CreateDirTask) Description() string { return t.Desc }
func (t *CreateDirTask) Priority() int       { return t.TaskPrio }
