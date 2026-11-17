// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"reflect"
	"testing"

	"scbake/internal/types"
)

// issue138DualHandler implements both the plain tooling handler interface and
// types.RegistryAwareHandler. GetTasks intentionally fails so a regression that
// stops preferring the registry-aware seam is loud instead of silent.
type issue138DualHandler struct {
	plainCalls    int
	registryCalls int
	targetPath    string
	templateDir   string
	cacheDir      string
	registries    []string
}

var (
	_ templateTaskProvider       = (*issue138DualHandler)(nil)
	_ types.RegistryAwareHandler = (*issue138DualHandler)(nil)
)

func (h *issue138DualHandler) GetTasks(targetPath, templateDir, registryCacheDir string) ([]types.Task, error) {
	h.plainCalls++
	h.targetPath = targetPath
	h.templateDir = templateDir
	h.cacheDir = registryCacheDir
	return nil, errors.New("plain GetTasks must not be called")
}

func (h *issue138DualHandler) GetTasksWithRegistries(targetPath, templateDir, registryCacheDir string, registries []string) ([]types.Task, error) {
	h.registryCalls++
	h.targetPath = targetPath
	h.templateDir = templateDir
	h.cacheDir = registryCacheDir
	h.registries = append([]string(nil), registries...)
	return nil, nil
}

// TestIssue138Revision_GetTemplateTasksPrefersRegistryAware is the fail-closed
// seam the review found untested: core must dispatch to
// GetTasksWithRegistries and forward RegistryNames, because maven_linter
// resolves its injected snippet at plan time. A regression to h.GetTasks fails
// here (both via the sentinel error and the recorded call counts).
func TestIssue138Revision_GetTemplateTasksPrefersRegistryAware(t *testing.T) {
	h := &issue138DualHandler{}
	rc := RunContext{
		TargetPath:       "project/target",
		TemplateDir:      "project/templates",
		RegistryCacheDir: "project/cache",
		RegistryNames:    []string{"zeta", "alpha"},
	}

	if _, err := getTemplateTasks(h, rc); err != nil {
		t.Fatalf("getTemplateTasks used the non-registry seam: %v", err)
	}
	if h.plainCalls != 0 {
		t.Errorf("GetTasks called %d times, want 0", h.plainCalls)
	}
	if h.registryCalls != 1 {
		t.Errorf("GetTasksWithRegistries calls = %d, want 1", h.registryCalls)
	}
	if h.targetPath != rc.TargetPath || h.templateDir != rc.TemplateDir || h.cacheDir != rc.RegistryCacheDir {
		t.Errorf("paths forwarded as (%q, %q, %q), want (%q, %q, %q)",
			h.targetPath, h.templateDir, h.cacheDir,
			rc.TargetPath, rc.TemplateDir, rc.RegistryCacheDir)
	}
	if !reflect.DeepEqual(h.registries, rc.RegistryNames) {
		t.Errorf("allowlist = %v, want %v (order preserved)", h.registries, rc.RegistryNames)
	}
}

// issue138DualLangHandler is the language-handler variant: its plain GetTasks
// takes explicit schema variables as well.
type issue138DualLangHandler struct {
	plainCalls    int
	registryCalls int
	vars          map[string]string
	registries    []string
}

var (
	_ langTaskProvider           = (*issue138DualLangHandler)(nil)
	_ types.RegistryAwareHandler = (*issue138DualLangHandler)(nil)
)

func (h *issue138DualLangHandler) GetTasks(_, _, _ string, vars map[string]string) ([]types.Task, error) {
	h.plainCalls++
	h.vars = vars
	return nil, errors.New("plain GetTasks must not be called")
}

func (h *issue138DualLangHandler) GetTasksWithRegistries(_, _, _ string, registries []string) ([]types.Task, error) {
	h.registryCalls++
	h.registries = append([]string(nil), registries...)
	return nil, nil
}

// TestIssue138Revision_GetLangTasksPrefersRegistryAware keeps the same
// fail-closed seam for language handlers.
func TestIssue138Revision_GetLangTasksPrefersRegistryAware(t *testing.T) {
	h := &issue138DualLangHandler{}
	rc := RunContext{
		RegistryNames: []string{"zeta", "alpha"},
		SetVars:       map[string]string{"oncall": "team"},
	}

	if _, err := getLangTasks(h, rc); err != nil {
		t.Fatalf("getLangTasks used the non-registry seam: %v", err)
	}
	if h.plainCalls != 0 {
		t.Errorf("GetTasks called %d times, want 0", h.plainCalls)
	}
	if h.registryCalls != 1 {
		t.Errorf("GetTasksWithRegistries calls = %d, want 1", h.registryCalls)
	}
	if !reflect.DeepEqual(h.registries, rc.RegistryNames) {
		t.Errorf("allowlist = %v, want %v (order preserved)", h.registries, rc.RegistryNames)
	}
}

// issue138PlainHandler implements only the plain tooling handler interface,
// proving the fallback still works for handlers that do not resolve registries
// at plan time.
type issue138PlainHandler struct {
	plainCalls int
}

var _ templateTaskProvider = (*issue138PlainHandler)(nil)

func (h *issue138PlainHandler) GetTasks(_, _, _ string) ([]types.Task, error) {
	h.plainCalls++
	return nil, nil
}

// TestIssue138Revision_GetTemplateTasksFallsBackForPlainHandler pins the
// non-registry-aware path.
func TestIssue138Revision_GetTemplateTasksFallsBackForPlainHandler(t *testing.T) {
	h := &issue138PlainHandler{}
	if _, err := getTemplateTasks(h, RunContext{RegistryNames: []string{"team"}}); err != nil {
		t.Fatalf("getTemplateTasks: %v", err)
	}
	if h.plainCalls != 1 {
		t.Errorf("GetTasks calls = %d, want 1", h.plainCalls)
	}
}

// issue138PlainLangHandler implements only the plain language handler
// interface, so the fallback must forward the explicit schema variables.
type issue138PlainLangHandler struct {
	plainCalls int
	vars       map[string]string
}

var _ langTaskProvider = (*issue138PlainLangHandler)(nil)

func (h *issue138PlainLangHandler) GetTasks(_, _, _ string, vars map[string]string) ([]types.Task, error) {
	h.plainCalls++
	h.vars = vars
	return nil, nil
}

// TestIssue138Revision_GetLangTasksFallsBackWithSetVars pins that the plain
// language path still receives the explicit --set values.
func TestIssue138Revision_GetLangTasksFallsBackWithSetVars(t *testing.T) {
	h := &issue138PlainLangHandler{}
	rc := RunContext{
		RegistryNames: []string{"team"},
		SetVars:       map[string]string{"oncall": "team"},
	}
	if _, err := getLangTasks(h, rc); err != nil {
		t.Fatalf("getLangTasks: %v", err)
	}
	if h.plainCalls != 1 {
		t.Errorf("GetTasks calls = %d, want 1", h.plainCalls)
	}
	if !reflect.DeepEqual(h.vars, rc.SetVars) {
		t.Errorf("vars = %v, want %v", h.vars, rc.SetVars)
	}
}
