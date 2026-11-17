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

// templateName is the registry template name this handler is registered under.
// It constrains registry cache lookups to this template's subdirectory.
const templateName = "maven_linter"

// Handler implements templates.Handler.
type Handler struct{}

// Handler resolves registry-cached templates at plan time, so it implements the
// optional types.RegistryAwareHandler interface used by core.
var _ types.RegistryAwareHandler = (*Handler)(nil)

// GetTasks returns the plan to create Checkstyle config and inject the plugin into pom.xml.
//
// It disables the registry tier (nil allowlist) and is retained for direct
// callers. Core calls GetTasksWithRegistries so a configured registry can
// override the injected snippet.
func (h *Handler) GetTasks(targetPath, templateDir, registryCacheDir string) ([]types.Task, error) {
	return h.GetTasksWithRegistries(targetPath, templateDir, registryCacheDir, nil)
}

// GetTasksWithRegistries returns the plan, resolving the injected plugin
// snippet from the registry cache only for registries in the allowlist.
func (h *Handler) GetTasksWithRegistries(_ string, templateDir string, registryCacheDir string, registries []string) ([]types.Task, error) {
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

	// Task 2: Inject the Checkstyle plugin into the existing pom.xml.
	// The handler is registered under "maven_linter", which is the registry
	// template name its cache lookup must be constrained to.
	pluginSnippet, err := tasks.ReadTemplate(templates, "pom_snippet.xml.tpl", templateDir, registryCacheDir, templateName, registries)
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
