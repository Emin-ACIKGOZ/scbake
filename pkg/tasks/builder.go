// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"embed"
	"fmt"
	"scbake/internal/types"
)

// TemplateFileSpec defines the declarative mapping for a template file.
type TemplateFileSpec struct {
	TemplatePath string
	OutputPath   string
	Desc         string
	TemplateData interface{}
}

// BuildTemplateTasks initializes a priority sequence within [prioStart, prioMax]
// and constructs CreateTemplateTask instances for each specified TemplateFileSpec.
func BuildTemplateTasks(efs embed.FS, prioStart, prioMax types.Priority, specs ...TemplateFileSpec) ([]types.Task, error) {
	seq, err := types.NewPrioritySequence(prioStart, prioMax)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	plan := make([]types.Task, 0, len(specs))
	for _, spec := range specs {
		p, err := seq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &CreateTemplateTask{
			TemplateFS:   efs,
			TemplatePath: spec.TemplatePath,
			OutputPath:   spec.OutputPath,
			Desc:         spec.Desc,
			TemplateData: spec.TemplateData,
			TaskPrio:     int(p),
		})
	}

	return plan, nil
}
