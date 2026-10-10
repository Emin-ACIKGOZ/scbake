// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

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
func (h *Handler) GetTasks(_ string, templateDir string, registryCacheDir string) ([]types.Task, error) {
	// Task 1: Create the Checkstyle config file using BuildTemplateTasks
	plan, err := tasks.BuildTemplateTasks(templates, types.PrioLinter, types.MaxLinter,
		tasks.TemplateFileSpec{
			TemplatePath: "checkstyle.xml.tpl",
			OutputPath:   "checkstyle.xml",
			Desc:         "Create Maven Checkstyle configuration",
		},
	)
	if err != nil {
		return nil, err
	}

	// Task 2: Inject the Checkstyle plugin into the existing pom.xml
	pluginSnippet, err := tasks.ReadTemplate(templates, "pom_snippet.xml.tpl", templateDir, registryCacheDir)
	if err != nil {
		return nil, err
	}

	plan = append(plan, &tasks.InsertXMLTask{
		FilePath:    "pom.xml",
		ElementPath: "/project/build/plugins",
		XMLContent:  string(pluginSnippet),
		Desc:        "Inject Maven Checkstyle plugin into pom.xml",
		TaskPrio:    int(types.PrioLinter + 1),
	})

	return plan, nil
}
