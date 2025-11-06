package tasks

import (
	"fmt"
	"scbake/internal/manifest"
	"scbake/internal/types"
)

// UpdateManifestTask modifies and saves the scbake.toml file.
type UpdateManifestTask struct {
	// Data to update the manifest with
	NewProject  *types.Project
	NewTemplate *types.Template

	Desc     string
	TaskPrio int
}

func (t *UpdateManifestTask) Description() string {
	return t.Desc
}

func (t *UpdateManifestTask) Priority() int {
	// This should run *last* to capture all successful changes.
	return 998
}

func (t *UpdateManifestTask) Execute(tc types.TaskContext) error {
	if tc.DryRun {
		return nil
	}

	// The manifest in the context is the *original* one.
	// We modify it here.
	if t.NewProject != nil {
		// We'll add logic to check for duplicates later
		tc.Manifest.Projects = append(tc.Manifest.Projects, *t.NewProject)
	}

	if t.NewTemplate != nil {
		tc.Manifest.Templates = append(tc.Manifest.Templates, *t.NewTemplate)
	}

	// Now, save the modified manifest to disk.
	// This is part of the transaction. If it fails, Git will roll it back.
	if err := manifest.Save(tc.Manifest); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	return nil
}
