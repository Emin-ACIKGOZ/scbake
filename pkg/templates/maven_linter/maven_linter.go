// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package mavenlinter

import (
	"embed"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

//go:embed checkstyle.xml.tpl pom_snippet.xml.tpl
var templates embed.FS

// Handler implements the templates.Handler interface for Maven linting.
type Handler struct{}

// GetTasks returns the plan to create Checkstyle config and inject the plugin into pom.xml.
func (h *Handler) GetTasks(_ string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequence for the Linter band (1200-1399)
	seq := types.NewPrioritySequence(types.PrioLinter, types.MaxLinter)

	// Task 1: Create the Checkstyle config file
	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "checkstyle.xml.tpl",
		OutputPath:   "checkstyle.xml",
		Desc:         "Create Maven Checkstyle configuration",
		TaskPrio:     int(p), // Now 1200
	})

	// Task 2: Inject the Checkstyle plugin into the existing pom.xml
	p, err = seq.Next()
	if err != nil {
		return nil, err
	}

	// Read the plugin snippet to inject
	pluginSnippet, err := templates.ReadFile("pom_snippet.xml.tpl")
	if err != nil {
		return nil, err
	}

	plan = append(plan, &tasks.InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  string(pluginSnippet),
		Desc:        "Inject Maven Checkstyle plugin into pom.xml",
		TaskPrio:    int(p), // Now 1201
	})

	return plan, nil
}
