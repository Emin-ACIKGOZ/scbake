// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"
	"scbake/internal/preflight"
	"scbake/internal/schema"
	"scbake/internal/types"
	"scbake/internal/util"
	"scbake/internal/util/fileutil"
	"scbake/pkg/lang"
	"scbake/pkg/tasks"
	"scbake/pkg/templates"
	"strings"
	"time"
)

const (
	// defaultTimeout is the maximum time allowed for the entire operation.
	// This includes external commands like spring boot init, npm install, etc.
	defaultTimeout = 30 * time.Minute
)

// ErrNoSelection is returned when neither a language pack nor any tooling
// template was requested, so there is nothing to apply.
var ErrNoSelection = errors.New("no language or templates specified")

// RunContext holds the flags and arguments for a single execution run.
type RunContext struct {
	LangFlag         string
	WithFlag         []string
	TargetPath       string // Absolute path for execution stability.
	ManifestPathArg  string // Relative path for manifest portability.
	DryRun           bool
	Force            bool
	ConflictStrategy string
	TemplateDir      string
	RegistryCacheDir string
	License          string
	CopyrightHolder  string
	SetVars          map[string]string

	// Update indicates a manifest-driven `update` rather than a flag-driven
	// `apply`. It restricts execution to managed file writes and skips
	// environment prerequisites, so re-running a project is safe.
	Update bool
}

// A struct to hold all proposed manifest changes
type manifestChanges struct {
	Projects  []types.Project
	Templates []types.Template
}

// RunApply orchestrates the template application process.
func RunApply(rc RunContext, reporter types.Reporter) error {
	reporter.Step("📖", "Loading manifest ("+fileutil.ManifestFileName+")...")

	// 1. Root Discovery & Manifest Load
	m, rootPath, err := manifest.Load(rc.TargetPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", fileutil.ManifestFileName, err)
	}

	// 2. Initialize Transaction Engine
	tx, cleanupTx, err := setupTransaction(rootPath, rc.DryRun)
	if err != nil {
		return err
	}
	defer cleanupTx()

	reporter.Step("📝", "Building execution plan...")

	// 3. Execution Plan Construction
	rc.WithFlag = deduplicateTemplates(rc.WithFlag)
	plan, _, changes, err := buildPlan(rc, rootPath)
	if err != nil {
		return err
	}

	// `update` reconciles managed files only. Re-running side-effecting tasks
	// (go mod init, npm install, ...) would fail or mutate the environment.
	if rc.Update {
		plan.Tasks = managedWriteTasks(plan.Tasks)
	}

	// 4. Build Task Context & Validate Schemas
	tc, cancelCtx, err := buildTaskContext(rc, m, changes, tx)
	if err != nil {
		return err
	}
	defer cancelCtx()

	if rc.DryRun {
		return Execute(plan, tc, reporter)
	}

	return executeAndFinalize(reporter, plan, tc, m, changes, rootPath, tx)
}

// RunUpdate re-applies the template selection recorded in the manifest to an
// already-generated project. It is `apply` driven by the manifest instead of by
// flags, which is what makes it idempotent and non-interactive.
func RunUpdate(rc RunContext, reporter types.Reporter) error {
	reporter.Step("📖", "Loading manifest ("+fileutil.ManifestFileName+")...")

	m, rootPath, err := manifest.Load(rc.TargetPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", fileutil.ManifestFileName, err)
	}

	lang, withTemplates, relPath, err := resolveUpdateSelection(m, rootPath, rc.TargetPath)
	if err != nil {
		return err
	}
	if lang == "" && len(withTemplates) == 0 {
		return fmt.Errorf("%s records no templates for %q; nothing to update", fileutil.ManifestFileName, rc.ManifestPathArg)
	}

	rc.LangFlag = lang
	rc.WithFlag = withTemplates
	rc.ManifestPathArg = relPath
	rc.Update = true

	return RunApply(rc, reporter)
}

// resolveUpdateSelection derives the language and tooling templates that were
// applied to targetPath from the manifest, along with the repo-relative path
// used to key them. Both the project and template entries must match the
// normalized path, so monorepo subprojects do not alias each other.
func resolveUpdateSelection(m *types.Manifest, rootPath, targetPath string) (string, []string, string, error) {
	if m == nil {
		return "", nil, "", errors.New("manifest is empty")
	}
	rel, err := repoRelativeProjectPath(rootPath, targetPath)
	if err != nil {
		return "", nil, "", err
	}

	var lang string
	for _, p := range m.Projects {
		if p.Path == rel {
			lang = p.Language
			break
		}
	}

	var with []string
	for _, t := range m.Templates {
		if t.Path == rel {
			with = append(with, t.Name)
		}
	}
	return lang, deduplicateTemplates(with), rel, nil
}

// managedWriteTasks keeps only file-reconciling tasks. `update` must not re-run
// environment or side-effecting tasks (go mod init, npm install, git hooks).
func managedWriteTasks(plan []types.Task) []types.Task {
	out := make([]types.Task, 0, len(plan))
	for _, t := range plan {
		if _, ok := t.(*tasks.CreateTemplateTask); ok {
			out = append(out, t)
		}
	}
	return out
}

// setupTransaction initializes the transaction safety net and returns a cleanup rollback deferral.
// If the program panics or returns an error at any point, the filesystem is restored to its original state.
// Calling tx.Commit() explicitly at the end disables the rollback safety net.
func setupTransaction(rootPath string, dryRun bool) (*transaction.Manager, func(), error) {
	if dryRun {
		return nil, func() {}, nil
	}

	tx, err := transaction.New(rootPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize transaction manager: %w", err)
	}

	// SAFETY: The defer ensures atomicity.
	cleanup := func() {
		if rErr := tx.Rollback(); rErr != nil {
			// We log this to stderr because we can't return it easily from defer
			// without named return parameters, and panic recovery is complex.
			// In a normal failure flow, Rollback is expected to succeed silently.
			fmt.Fprintf(os.Stderr, "⚠️  Transaction rollback warning: %v\n", rErr)
		}
	}
	return tx, cleanup, nil
}

// buildTaskContext constructs future manifest state and validates template schemas against metadata.
func buildTaskContext(
	rc RunContext,
	m *types.Manifest,
	changes *manifestChanges,
	tx *transaction.Manager,
) (types.TaskContext, func(), error) {
	// Deep copy manifest to ensure modifications don't affect original until finalization
	futureManifest := m.DeepCopy()

	// Update metadata from context if provided
	if rc.License != "" || rc.CopyrightHolder != "" || len(rc.SetVars) > 0 {
		if futureManifest.Metadata == nil {
			futureManifest.Metadata = make(map[string]string)
		}
		if rc.License != "" {
			futureManifest.Metadata["license"] = rc.License
		}
		if rc.CopyrightHolder != "" {
			futureManifest.Metadata["copyright_holder"] = rc.CopyrightHolder
		}
		for k, v := range rc.SetVars {
			futureManifest.Metadata[k] = v
		}
	}

	futureManifest.Projects = append(futureManifest.Projects, changes.Projects...)
	futureManifest.Templates = append(futureManifest.Templates, changes.Templates...)

	// Reject untrusted manifest fields before any template renders them. Project
	// paths/names are interpolated into generated shell scripts, Makefile
	// recipes, and YAML, so an unsafe value must fail the plan rather than reach
	// the templates (SCB-006/SCB-007/SCB-008).
	if err := validateManifestFields(futureManifest); err != nil {
		return types.TaskContext{}, nil, err
	}

	// Validate all selected templates against their schemas before executing
	if err := validateSelectedTemplates(rc, futureManifest.Metadata); err != nil {
		return types.TaskContext{}, nil, err
	}

	// Prepare task context with default timeout
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	tc := types.TaskContext{
		Ctx:              ctx,
		DryRun:           rc.DryRun,
		Manifest:         futureManifest,
		TargetPath:       rc.TargetPath,
		Force:            rc.Force,
		ConflictStrategy: rc.ConflictStrategy,
		TemplateDir:      rc.TemplateDir,
		RegistryCacheDir: rc.RegistryCacheDir,
		Tx:               tx,
	}

	return tc, cancel, nil
}

// executeAndFinalize executes the planned tasks, updates the manifest, and commits the transaction.
func executeAndFinalize(
	reporter types.Reporter,
	plan *types.Plan,
	tc types.TaskContext,
	m *types.Manifest,
	changes *manifestChanges,
	rootPath string,
	tx *transaction.Manager,
) error {
	reporter.Step("🚀", "Executing plan...")
	if err := Execute(plan, tc, reporter); err != nil {
		return err
	}

	reporter.Step("✍️", "Updating manifest...")
	if err := persistManifestState(m, tc, changes, rootPath, tx); err != nil {
		return err
	}

	reporter.Step("✅", "Committing transaction...")
	// Point of No Return: We delete the backups.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// persistManifestState updates metadata, tracks scbake.toml in transaction, and saves manifest to disk.
func persistManifestState(
	m *types.Manifest,
	tc types.TaskContext,
	changes *manifestChanges,
	rootPath string,
	tx *transaction.Manager,
) error {
	if err := updateManifest(m, changes); err != nil {
		return err
	}

	// Persist metadata
	if tc.Manifest.Metadata != nil {
		if m.Metadata == nil {
			m.Metadata = make(map[string]string)
		}
		for k, v := range tc.Manifest.Metadata {
			m.Metadata[k] = v
		}
	}

	// Persist managed files state
	if tc.Manifest.ManagedFiles != nil {
		if m.ManagedFiles == nil {
			m.ManagedFiles = make(map[string]string)
		}
		for k, v := range tc.Manifest.ManagedFiles {
			m.ManagedFiles[k] = v
		}
	}

	// We track the manifest file itself before saving.
	// This ensures that if the Save succeeds but a subsequent step crashes (unlikely),
	// the manifest is rolled back to sync with the filesystem.
	manifestPath := filepath.Join(rootPath, fileutil.ManifestFileName)
	if err := tx.Track(manifestPath); err != nil {
		return fmt.Errorf("failed to track manifest file: %w", err)
	}

	if err := manifest.Save(m, rootPath); err != nil {
		return fmt.Errorf("manifest save failed: %w", err)
	}
	return nil
}

// updateManifest merges new projects and templates into the existing manifest
// structure. New projects are validated again here as a final guard so an
// unsafe field can never be persisted even if it slipped past the plan-time
// check.
func updateManifest(m *types.Manifest, changes *manifestChanges) error {
	for _, newProj := range changes.Projects {
		if err := validateProject(newProj); err != nil {
			return err
		}
	}

	// Update Projects (ensure no duplicates by path)
	existingProjects := make(map[string]bool)
	for _, proj := range m.Projects {
		existingProjects[proj.Path] = true
	}
	for _, newProj := range changes.Projects {
		if !existingProjects[newProj.Path] {
			m.Projects = append(m.Projects, newProj)
		}
	}
	// Update Templates (ensure no duplicates by name + path)
	existingTemplates := make(map[string]bool)
	for _, tmpl := range m.Templates {
		key := tmpl.Name + ":" + tmpl.Path
		existingTemplates[key] = true
	}
	for _, newTmpl := range changes.Templates {
		key := newTmpl.Name + ":" + newTmpl.Path
		if !existingTemplates[key] {
			m.Templates = append(m.Templates, newTmpl)
		}
	}

	return nil
}

// validateManifestFields rejects untrusted manifest fields that the generated
// templates interpolate into interpreted contexts (shell scripts, Makefile
// recipes, and YAML). It is called at plan time, before any template renders,
// and fails closed: a project whose path/name/language is not a plain,
// repo-relative identifier aborts the whole run. See SCB-006, SCB-007, SCB-008.
func validateManifestFields(m *types.Manifest) error {
	if m == nil {
		return nil
	}
	for i := range m.Projects {
		if err := validateProject(m.Projects[i]); err != nil {
			return err
		}
	}
	if cmd, ok := m.Metadata["test_command"]; ok {
		if err := validateTestCommand(cmd); err != nil {
			return fmt.Errorf("metadata test_command is invalid: %w", err)
		}
	}
	return nil
}

// validateProject checks every untrusted field of a single project. The error
// names the offending project so the user can locate it in scbake.toml.
func validateProject(p types.Project) error {
	if err := validateProjectPath(p.Path); err != nil {
		return fmt.Errorf("project %q has an invalid path %q: %w", p.Name, p.Path, err)
	}
	if err := validateProjectName(p.Name); err != nil {
		return fmt.Errorf("project %q has an invalid name: %w", p.Name, err)
	}
	if err := validateProjectLanguage(p.Language); err != nil {
		return fmt.Errorf("project %q has an invalid language %q: %w", p.Name, p.Language, err)
	}
	return nil
}

// validateProjectPath enforces a clean, repo-relative project path. The path is
// copied verbatim into generated `cd <path>` shell commands and YAML
// `directory:` values, so it must not contain anything that could escape those
// scalar contexts: absolute paths, `..` traversal, backslashes, whitespace,
// shell/YAML metacharacters, or control characters/newlines.
func validateProjectPath(p string) error {
	switch {
	case p == "":
		return errors.New("path must not be empty")
	case strings.ContainsRune(p, '\\'):
		return errors.New("path must not contain backslashes")
	case path.IsAbs(p), strings.HasPrefix(p, "/"):
		return errors.New("path must be relative to the repository root")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return errors.New("path must not contain control characters or newlines")
		}
		if !isSafePathRune(r) {
			return fmt.Errorf("path contains an unsafe character %q", r)
		}
	}
	for _, comp := range strings.Split(p, "/") {
		if comp == ".." {
			return errors.New("path must not contain '..' components")
		}
	}
	return nil
}

// validateProjectName enforces a safe identifier for the project name, which is
// interpolated into shell `echo` lines and YAML/comment text. Shell/YAML
// metacharacters and newlines are rejected.
func validateProjectName(n string) error {
	if n == "" {
		return errors.New("name must not be empty")
	}
	return validateIdentifier(n, "name")
}

// validateProjectLanguage enforces a safe identifier for the language field,
// which is embedded in Makefile variable names (MAKE_BUILD_COMMAND_<lang>) and
// shell/YAML text. An empty language is allowed (it yields no build command).
func validateProjectLanguage(l string) error {
	if l == "" {
		return nil
	}
	return validateIdentifier(l, "language")
}

// validateIdentifier rejects shell/YAML metacharacters and control characters
// in a value that must be a plain ASCII identifier.
func validateIdentifier(value, field string) error {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s must not contain control characters or newlines", field)
		}
		if !isSafeIdentifierRune(r) {
			return fmt.Errorf("%s contains an unsafe character %q", field, r)
		}
	}
	return nil
}

// isSafePathRune reports whether r is allowed in a repo-relative path: ASCII
// letters, digits, and the separators `.`, `_`, `-`, `/`.
func isSafePathRune(r rune) bool {
	return isSafeIdentifierRune(r) || r == '/'
}

// isSafeIdentifierRune reports whether r is allowed in a project name or
// language identifier: ASCII letters, digits, and `.`, `_`, `-`.
func isSafeIdentifierRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '_', r == '-':
		return true
	default:
		return false
	}
}

// validateTestCommand rejects control characters (notably newlines and tabs) in
// the free-form test_command metadata value. The value is intentionally
// executed as-is by the generated Makefile `test` recipe, but a newline or tab
// would let a committed manifest break out of that recipe and inject extra
// commands.
func validateTestCommand(cmd string) error {
	for _, r := range cmd {
		if r < 0x20 || r == 0x7f {
			return errors.New("test_command must not contain control characters or newlines")
		}
	}
	return nil
}

// repoRelativeProjectPath converts a user-supplied project path into a clean,
// slash-separated path relative to the manifest root. Relative inputs are
// cleaned in place; absolute inputs are made relative to rootPath so the
// manifest stays portable and the path validator can require a repo-relative
// value.
func repoRelativeProjectPath(rootPath, pathArg string) (string, error) {
	if pathArg == "" {
		return "", errors.New("project path must not be empty")
	}
	rel := pathArg
	if filepath.IsAbs(pathArg) {
		r, err := filepath.Rel(rootPath, pathArg)
		if err != nil {
			return "", fmt.Errorf("cannot make path %q relative to %q: %w", pathArg, rootPath, err)
		}
		rel = r
	}
	return filepath.ToSlash(filepath.Clean(rel)), nil
}

// validateSelectedTemplates checks every requested template and language
// handler's schema against the provided metadata. It collects all errors and
// reports them at once so the user can fix everything in a single pass.
// Language handlers that implement SchemaProvider are also validated.
//
//nolint:cyclop // Two independent branches (with + lang) each have standard checks.
func validateSelectedTemplates(rc RunContext, metadata map[string]string) error {
	var allErrors []string

	// Validate --with template handlers
	for _, name := range rc.WithFlag {
		efs, path, err := templates.GetSchema(name)
		if err != nil {
			return fmt.Errorf("schema lookup for %q: %w", name, err)
		}
		if efs == nil {
			continue
		}

		s, err := schema.ReadSchema(*efs, path)
		if err != nil {
			return fmt.Errorf("failed to read schema for %q: %w", name, err)
		}

		result := schema.Validate(s, metadata)
		if result.HasErrors() {
			allErrors = append(allErrors, fmt.Sprintf("template %q:\n%s", name, result.Error()))
		}
	}

	// Validate --lang handlers that implement SchemaProvider
	if rc.LangFlag != "" {
		efs, path, err := lang.GetSchema(rc.LangFlag)
		if err != nil {
			return fmt.Errorf("schema lookup for %q: %w", rc.LangFlag, err)
		}
		if efs != nil {
			s, rErr := schema.ReadSchema(*efs, path)
			if rErr != nil {
				return fmt.Errorf("failed to read schema for %q: %w", rc.LangFlag, rErr)
			}

			result := schema.Validate(s, metadata)
			if result.HasErrors() {
				allErrors = append(allErrors, fmt.Sprintf("language %q:\n%s", rc.LangFlag, result.Error()))
			}
		}
	}

	if len(allErrors) > 0 {
		var b strings.Builder
		b.WriteString("input validation failed:\n")
		for _, e := range allErrors {
			b.WriteString(e)
		}
		return fmt.Errorf("%s", b.String())
	}

	return nil
}

// stampTemplateName records which registry template each CreateTemplateTask
// renders. Handlers are selected by name but their tasks do not carry it, so
// the plan builder fills it in here; registry cache resolution then stays
// constrained to the requested template. Tasks that already name a template are
// left untouched.
func stampTemplateName(taskList []types.Task, templateName string) {
	for _, task := range taskList {
		ct, ok := task.(*tasks.CreateTemplateTask)
		if !ok || ct.TemplateName != "" {
			continue
		}
		ct.TemplateName = templateName
	}
}

func deduplicateTemplates(requested []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, t := range requested {
		if !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	return result
}

// buildPlan constructs the list of tasks based on CLI flags.
func buildPlan(rc RunContext, rootPath string) (*types.Plan, string, *manifestChanges, error) {
	plan := &types.Plan{Tasks: []types.Task{}}
	changes := &manifestChanges{}
	commitMessage := "scbake: Apply templates"
	didSomething := false

	if rc.LangFlag != "" {
		didSomething = true
		msg, err := handleLangFlag(rc, rootPath, plan, changes)
		if err != nil {
			return nil, "", nil, err
		}
		commitMessage = msg
	}

	if len(rc.WithFlag) > 0 {
		didSomething = true
		if err := handleWithFlag(rc, plan, changes); err != nil {
			return nil, "", nil, err
		}
		// Update commit message based on what was done
		if rc.LangFlag == "" {
			commitMessage = fmt.Sprintf("scbake: Apply templates (%v) to %s", rc.WithFlag, rc.ManifestPathArg)
		}
	}

	// Only fail if neither a language nor tooling was specified.
	if !didSomething {
		return nil, "", nil, ErrNoSelection
	}

	return plan, commitMessage, changes, nil
}

// checkLangPrerequisites validates that required binaries are available for the language.
func checkLangPrerequisites(langFlag string) error {
	switch langFlag {
	case "go":
		return preflight.CheckBinaries("go")
	case "svelte":
		return preflight.CheckBinaries("npm")
	case "spring":
		return preflight.CheckBinaries("curl", "unzip", "java")
	}
	return nil
}

// handleLangFlag processes the --lang flag, adding language tasks and project info.
func handleLangFlag(rc RunContext, rootPath string, plan *types.Plan, changes *manifestChanges) (string, error) {
	// `update` only writes managed files, so language toolchains (go, npm,
	// java) need not be installed.
	if !rc.Update {
		if err := checkLangPrerequisites(rc.LangFlag); err != nil {
			return "", err
		}
	}

	handler, err := lang.GetHandler(rc.LangFlag)
	if err != nil {
		return "", err
	}

	langTasks, err := handler.GetTasks(rc.TargetPath, rc.TemplateDir, rc.RegistryCacheDir)
	if err != nil {
		return "", fmt.Errorf("failed to get tasks for lang '%s': %w", rc.LangFlag, err)
	}

	if len(langTasks) == 0 {
		return "", fmt.Errorf("language handler '%s' returned no tasks (possible misconfiguration)", rc.LangFlag)
	}

	stampTemplateName(langTasks, rc.LangFlag)
	plan.Tasks = append(plan.Tasks, langTasks...)

	projectName, err := util.SanitizeModuleName(rc.ManifestPathArg)
	if err != nil {
		return "", fmt.Errorf("could not determine project name: %w", err)
	}

	// Store a repo-relative path (the manifest is documented as portable) and
	// validate it before it can reach any template.
	projectPath, err := repoRelativeProjectPath(rootPath, rc.ManifestPathArg)
	if err != nil {
		return "", fmt.Errorf("could not determine project path: %w", err)
	}

	project := types.Project{
		Name:     projectName,
		Path:     projectPath,
		Language: rc.LangFlag,
	}
	if err := validateProject(project); err != nil {
		return "", err
	}
	changes.Projects = append(changes.Projects, project)

	return fmt.Sprintf("scbake: Apply '%s' to %s", rc.LangFlag, rc.ManifestPathArg), nil
}

// handleWithFlag processes the --with flag, adding template tasks.
func handleWithFlag(rc RunContext, plan *types.Plan, changes *manifestChanges) error {
	for _, tmplName := range rc.WithFlag {
		handler, err := templates.GetHandler(tmplName)
		if err != nil {
			return err
		}

		tmplTasks, err := handler.GetTasks(rc.TargetPath, rc.TemplateDir, rc.RegistryCacheDir)
		if err != nil {
			return fmt.Errorf("failed to get tasks for template '%s': %w", tmplName, err)
		}

		stampTemplateName(tmplTasks, tmplName)

		if len(tmplTasks) > 0 {
			plan.Tasks = append(plan.Tasks, tmplTasks...)
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  Template handler '%s' returned no tasks (possible misconfiguration)\n", tmplName)
		}

		// Record the real template reference (name + manifest-relative path) so
		// `update` can reproduce what was applied (SCB-064, D2). The previous
		// placeholder ({name: "root-templates"}) could not.
		changes.Templates = append(changes.Templates, types.Template{
			Name: tmplName,
			Path: rc.ManifestPathArg,
		})
	}

	return nil
}
