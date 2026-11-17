// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"strconv"
	"strings"
)

//go:embed main.go.tpl gitignore.tpl schema.json
var templates embed.FS

const (
	// goModSubcommand is the `go mod` subcommand used for module management.
	goModSubcommand = "mod"

	goModFile = "go.mod"
	goSumFile = "go.sum"
)

// Handler implements lang.Handler.
type Handler struct{}

// SchemaFS implements lang.SchemaProvider.
func (h *Handler) SchemaFS() embed.FS { return templates }

// SchemaPath implements lang.SchemaProvider.
func (h *Handler) SchemaPath() string { return "schema.json" }

// GetTasks returns the execution plan for a Go project. vars carries only the
// explicit --set values for this invocation; schema defaults are never included
// and an empty value means "no override".
func (h *Handler) GetTasks(targetPath string, _ string, _ string, vars map[string]string) ([]types.Task, error) {
	// Use a single sequence for all language setup tasks (100-999)
	langSeq, err := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority sequence: %w", err)
	}

	plan, err := appendBaseTasks(nil, langSeq)
	if err != nil {
		return nil, err
	}

	goModPath := filepath.Join(targetPath, goModFile)
	_, checkErr := os.Stat(goModPath)

	if os.IsNotExist(checkErr) {
		return appendInitTasks(plan, langSeq, targetPath, vars)
	}
	if checkErr == nil {
		return appendMaintenanceTasks(plan, langSeq, targetPath, vars)
	}
	return nil, fmt.Errorf("could not check for go.mod: %w", checkErr)
}

// appendBaseTasks appends the templates every Go project receives.
func appendBaseTasks(plan []types.Task, seq *types.PrioritySequence) ([]types.Task, error) {
	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "gitignore.tpl",
		OutputPath:   fileutil.GitIgnore,
		Desc:         "Create " + fileutil.GitIgnore,
		TaskPrio:     int(p),
	})

	p, err = seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateTemplateTask{
		TemplateFS:   templates,
		TemplatePath: "main.go.tpl",
		OutputPath:   "main.go",
		Desc:         "Create main.go",
		TaskPrio:     int(p),
	})

	return plan, nil
}

// appendInitTasks adds module initialization for a project without go.mod.
func appendInitTasks(plan []types.Task, seq *types.PrioritySequence, targetPath string, vars map[string]string) ([]types.Task, error) {
	moduleName, err := moduleNameFor(targetPath, vars)
	if err != nil {
		return nil, fmt.Errorf("could not determine module name: %w", err)
	}

	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:              "go",
		Args:             []string{goModSubcommand, "init", moduleName},
		Desc:             "Run go mod init " + moduleName,
		TaskPrio:         int(p),
		RunInTarget:      true,
		PredictedCreated: []string{goModFile},
	})

	plan, err = appendGoDirectiveTask(plan, seq, vars)
	if err != nil {
		return nil, err
	}

	return appendTidyTask(plan, seq, "Run go mod tidy")
}

// appendMaintenanceTasks adds jobs for a project that already has go.mod.
func appendMaintenanceTasks(plan []types.Task, seq *types.PrioritySequence, targetPath string, vars map[string]string) ([]types.Task, error) {
	// scbake never rewrites an existing module path; a differing explicit
	// value is an error so it is never recorded without being honored.
	if requested := vars["module_path"]; requested != "" {
		if err := ensureModulePathUnchanged(targetPath, requested); err != nil {
			return nil, err
		}
	}

	plan, err := appendGoDirectiveTask(plan, seq, vars)
	if err != nil {
		return nil, err
	}

	return appendTidyTask(plan, seq, "Run go mod tidy (project exists)")
}

// appendTidyTask appends `go mod tidy`.
func appendTidyTask(plan []types.Task, seq *types.PrioritySequence, desc string) ([]types.Task, error) {
	args := []string{goModSubcommand, "tidy"}

	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:              "go",
		Args:             args,
		Desc:             desc,
		TaskPrio:         int(p),
		RunInTarget:      true,
		PredictedCreated: []string{goModFile, goSumFile},
	})

	return plan, nil
}

// moduleNameFor returns the explicit module_path when set, otherwise the
// sanitized directory name. Go remains authoritative for module-path grammar;
// only flag-like and whitespace values are rejected here so the error is clear
// and raised at plan time instead of after `go mod init` runs.
func moduleNameFor(targetPath string, vars map[string]string) (string, error) {
	explicit := vars["module_path"]
	if explicit == "" {
		return util.SanitizeModuleName(targetPath)
	}

	if strings.HasPrefix(explicit, "-") {
		return "", fmt.Errorf("invalid module_path %q: leading dash", explicit)
	}
	if strings.TrimSpace(explicit) != explicit || strings.ContainsAny(explicit, " \t\r\n") {
		return "", fmt.Errorf("invalid module_path %q: whitespace is not allowed", explicit)
	}

	return explicit, nil
}

// appendGoDirectiveTask appends `go mod edit -go=<v>` when go_version is
// explicitly set. go.mod is declared so a transaction restores an existing
// module on rollback. No task is added when the value is absent or empty.
func appendGoDirectiveTask(plan []types.Task, seq *types.PrioritySequence, vars map[string]string) ([]types.Task, error) {
	version := vars["go_version"]
	if version == "" {
		return plan, nil
	}

	p, err := seq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:              "go",
		Args:             []string{goModSubcommand, "edit", "-go=" + version},
		Desc:             "Run go mod edit -go=" + version,
		TaskPrio:         int(p),
		RunInTarget:      true,
		PredictedCreated: []string{goModFile},
	})

	return plan, nil
}

// ensureModulePathUnchanged errors when an explicit module_path differs from the
// module directive already in go.mod. scbake never rewrites an existing module.
func ensureModulePathUnchanged(targetPath, requested string) error {
	current, err := moduleDirective(targetPath)
	if err != nil {
		return err
	}
	if current == requested {
		return nil
	}

	return fmt.Errorf(
		"go.mod already declares module %q; scbake will not rewrite an existing module path (requested %q). To change it, run: go mod edit -module=%s",
		current, requested, requested,
	)
}

// moduleDirective returns the path declared by the module directive.
func moduleDirective(targetPath string) (string, error) {
	data, err := fileutil.SafeReadFile(targetPath, goModFile)
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", goModFile, err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "module" {
			continue
		}
		modulePath, err := unquoteModulePath(fields[1])
		if err != nil {
			return "", fmt.Errorf("%s has a malformed module directive: %w", goModFile, err)
		}
		return modulePath, nil
	}

	return "", fmt.Errorf("%s has no module directive", goModFile)
}

// unquoteModulePath returns the module path from a go.mod token. Go's modfile
// syntax accepts a bare path or a double-quoted interpreted string; raw-string
// (backtick) quoting is not valid in go.mod, so compare only against forms Go
// itself accepts.
func unquoteModulePath(token string) (string, error) {
	if strings.Contains(token, "`") {
		return "", fmt.Errorf("module path %q uses backtick quoting, which go.mod does not accept; use a double-quoted string", token)
	}
	if !strings.HasPrefix(token, `"`) {
		if strings.Contains(token, `"`) {
			return "", fmt.Errorf("unexpected quote in module path %q", token)
		}
		return token, nil
	}

	modulePath, err := strconv.Unquote(token)
	if err != nil {
		return "", fmt.Errorf("could not unquote module path %q: %w", token, err)
	}
	return modulePath, nil
}
