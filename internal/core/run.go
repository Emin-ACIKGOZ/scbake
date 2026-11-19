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
	"scbake/pkg/templates/compliance"
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

// legacyRootTemplate is the placeholder name written by manifests generated
// before SCB-064 recorded real template references. `update` skips it rather
// than failing with an unknown-template error.
const legacyRootTemplate = "root-templates"

// gitTemplateName is the tooling template that creates a repository and its
// initial commit.
const gitTemplateName = "git"

// RunContext holds the flags and arguments for a single execution run.
type RunContext struct {
	LangFlag   string
	WithFlag   []string
	TargetPath string // The run's target path; relative values resolve against the process working directory.
	// ManifestPathArg is the raw CLI path argument. It is used for commit text
	// and diagnostics only, never as a canonical manifest path or project name:
	// runUpdate repurposes it to the normalised update-selection path. Manifest
	// paths and project names are computed from TargetPath.
	ManifestPathArg  string
	DryRun           bool
	Force            bool
	ConflictStrategy string
	TemplateDir      string
	RegistryCacheDir string
	// RegistryNames is the set of configured registry names. Registry cache
	// resolution is limited to these names; an empty set disables the registry
	// tier (fail closed).
	RegistryNames   []string
	License         string
	CopyrightHolder string
	SetVars         map[string]string

	// Ctx is the caller's cancellation context. It is typically the CLI signal
	// context; nil means context.Background(). buildTaskContext derives the
	// per-run timeout from it, so signals reach running tasks.
	Ctx context.Context

	// AllowAncestorSearch permits finding scbake.toml above a marker-less
	// start directory. It is set by the global --allow-ancestor-search flag.
	AllowAncestorSearch bool

	// Update indicates a manifest-driven `update` rather than a flag-driven
	// `apply`. It restricts execution to managed file writes and skips
	// environment prerequisites, so re-running a project is safe.
	Update bool

	// NewProject marks the initial `new` run. If it is interrupted before
	// commit, recovery removes the bootstrap project directory as well as the
	// transaction's tracked files.
	NewProject bool

	// DryRunWrites optionally receives the canonical managed-file writes a dry
	// run would perform. It is nil for real runs. Prediction callers supply a
	// map so a preview can distinguish findings the scoped plan repairs from
	// findings it leaves behind (for example another project's missing file).
	DryRunWrites map[string]types.PlannedWrite
}

// A struct to hold all proposed manifest changes
type manifestChanges struct {
	Projects  []types.Project
	Templates []types.Template
}

// RunApply orchestrates the template application process.
func RunApply(rc RunContext, reporter types.Reporter) error {
	// Git prerequisites are checked before the root lock so a missing identity
	// or a skip decision fails before lock acquisition creates .scbake. The
	// check is read-only and needs only the target path.
	if err := preflightGit(rc); err != nil {
		return err
	}
	return withRootLock(rc, func() error {
		return runApply(rc, reporter)
	})
}

// loadManifestForRun resolves the project root and manifest for a run. A
// dry-run `new` has not created its project directory yet, so it plans against
// an empty manifest rooted at the directory the real run would create; loading
// a parent manifest would leak projects, metadata, or an invalid path into the
// preview.
func loadManifestForRun(rc RunContext) (*types.Manifest, string, error) {
	if rc.NewProject && rc.DryRun {
		root, err := filepath.Abs(rc.TargetPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to resolve target path: %w", err)
		}
		return &types.Manifest{SbakeVersion: manifest.DefaultSbakeVersion}, root, nil
	}
	m, root, err := manifest.LoadWithOptions(rc.TargetPath, manifest.DiscoveryOptions{
		AllowAncestorSearch: rc.AllowAncestorSearch,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to load %s: %w", fileutil.ManifestFileName, err)
	}
	return m, root, nil
}

// runApply is RunApply without the root lock, so callers that already hold the
// lock can run the apply pipeline.
func runApply(rc RunContext, reporter types.Reporter) error {
	reporter.Step("📖", "Loading manifest ("+fileutil.ManifestFileName+")...")

	// 1. Root Discovery & Manifest Load
	m, rootPath, err := loadManifestForRun(rc)
	if err != nil {
		return err
	}
	return runApplyWithManifest(rc, reporter, m, rootPath)
}

// runApplyWithManifest continues the apply pipeline with a manifest the caller
// already loaded and reported. It emits no load step, so a caller that shares
// this pipeline (update) reports the load phase exactly once (#271).
func runApplyWithManifest(
	rc RunContext,
	reporter types.Reporter,
	m *types.Manifest,
	rootPath string,
) error {
	// Compute the canonical root-relative path of the target once from
	// TargetPath, resolving it against the process working directory when it is
	// relative. Both the project and template entries use this value, so every
	// accepted spelling and working directory records the same path. Deriving
	// it from ManifestPathArg would double-prefix an update launched from a
	// subdirectory, because runUpdate repurposes that field to the
	// already-normalised selection path.
	projectPath, err := repoRelativeProjectPath(rootPath, rc.TargetPath)
	if err != nil {
		return fmt.Errorf("could not determine project path: %w", err)
	}

	// Git identity and skip preflight ran in RunApply before the root lock so
	// acquisition cannot leave .scbake behind on a prerequisite failure.
	// `update` only reconciles managed files and never initializes a repository.

	// 2. Initialize Transaction Engine
	tx, cleanupTx, err := prepareRunTransaction(rc, rootPath)
	if err != nil {
		return err
	}
	defer cleanupTx()

	reporter.Step("📝", "Building execution plan...")

	// 3. Execution Plan Construction
	rc.WithFlag = deduplicateTemplates(rc.WithFlag)
	plan, _, changes, err := buildPlan(rc, rootPath, projectPath)
	if err != nil {
		return err
	}

	// `update` reconciles managed files only. Re-running side-effecting tasks
	// (go mod init, npm install, ...) would fail or mutate the environment.
	// Filter unmanaged paths at plan time so `--dry-run` and the real run agree.
	projectRel, err := repoRelativeProjectPath(rootPath, rc.TargetPath)
	if err != nil {
		return err
	}
	if rc.Update {
		plan.Tasks = managedWriteTasks(plan.Tasks)
		plan.Tasks = filterUnmanagedTasks(plan.Tasks, m, projectRel)
	} else if !rc.Force {
		// `apply` skips detached paths unless --force re-manages them.
		plan.Tasks = filterUnmanagedTasks(plan.Tasks, m, projectRel)
	}

	// 4. Build Task Context & Validate Schemas
	tc, cancelCtx, err := buildTaskContext(rc, m, changes, tx, rootPath)
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
	return withRootLock(rc, func() error {
		return runUpdate(rc, reporter)
	})
}

// runUpdate is RunUpdate without the root lock. It loads the manifest once,
// reports that phase itself, and hands the loaded manifest to the shared apply
// pipeline so the run reads and prints the load step exactly once (#271).
func runUpdate(rc RunContext, reporter types.Reporter) error {
	reporter.Step("📖", "Loading manifest ("+fileutil.ManifestFileName+")...")

	m, rootPath, err := loadManifestForRun(rc)
	if err != nil {
		return err
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

	return runApplyWithManifest(rc, reporter, m, rootPath)
}

// withRootLock serializes mutating runs against the same project root. Only
// `new` may create a missing root; apply/update must fail before creating an
// empty directory. Dry runs stay lock-free so they can inspect a project while
// another process writes; their existing pending-transaction check remains the
// consistency guard. The deferred release is registered before runApply/runUpdate
// defer their rollback, so commit or rollback finishes before the lock is
// released.
func withRootLock(rc RunContext, fn func() error) error {
	if rc.DryRun {
		return fn()
	}

	root, err := manifest.FindProjectRootWithOptions(rc.TargetPath, manifest.DiscoveryOptions{
		AllowAncestorSearch: rc.AllowAncestorSearch,
	})
	if err != nil {
		return fmt.Errorf("failed to find project root: %w", err)
	}

	lock, err := transaction.AcquireRootLock(root, rc.NewProject)
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Release()
	}()

	return fn()
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
		if t.Path == rel && t.Name != legacyRootTemplate {
			with = append(with, t.Name)
		}
	}
	return lang, deduplicateTemplates(with), rel, nil
}

// managedFileWriter marks a task that reconciles a file tracked in the
// managed_files ledger. `update` and `drift --repair` replay only these tasks;
// tasks without the marker are either side-effecting or write files outside the
// ledger (AppendFileTask, InsertXMLTask) and must not be re-run.
type managedFileWriter interface {
	WritesManagedFile() bool
}

// managedPathWriter is a managed writer whose target-relative output path is
// known before execution. The plan filter joins it with the project's
// root-relative path to match unmanaged_files, so an update/apply dry-run and
// the real run agree on which tasks are skipped.
type managedPathWriter interface {
	ManagedOutputPath() string
}

// Compile-time guards: a managed writer that loses its output path would
// otherwise stop being excluded from update/repair for unmanaged keys.
var (
	_ managedFileWriter = (*tasks.CreateTemplateTask)(nil)
	_ managedPathWriter = (*tasks.CreateTemplateTask)(nil)
	_ managedFileWriter = (*compliance.LicenseTask)(nil)
	_ managedPathWriter = (*compliance.LicenseTask)(nil)
)

// managedWriteTasks keeps only file-reconciling tasks. `update` must not re-run
// environment or side-effecting tasks (go mod init, npm install, git hooks).
func managedWriteTasks(plan []types.Task) []types.Task {
	out := make([]types.Task, 0, len(plan))
	for _, t := range plan {
		if w, ok := t.(managedFileWriter); ok && w.WritesManagedFile() {
			out = append(out, t)
		}
	}
	return out
}

// filterUnmanagedTasks removes managed writers whose root-relative output path
// is listed in unmanaged_files. Filtering happens at plan time so `--dry-run`
// does not list a task the real run skips. Tasks without a known managed output
// path are kept; the write path performs its own execution-level guard.
// projectRel is the project's slash-separated path relative to the manifest
// root ("." for the root project).
func filterUnmanagedTasks(plan []types.Task, m *types.Manifest, projectRel string) []types.Task {
	if m == nil || len(m.UnmanagedFiles) == 0 {
		return plan
	}
	unmanaged := make(map[string]struct{}, len(m.UnmanagedFiles))
	for _, key := range m.UnmanagedFiles {
		unmanaged[key] = struct{}{}
	}

	out := make([]types.Task, 0, len(plan))
	for _, t := range plan {
		w, ok := t.(managedPathWriter)
		if !ok {
			out = append(out, t)
			continue
		}
		key := filepath.ToSlash(filepath.Clean(path.Join(projectRel, w.ManagedOutputPath())))
		if _, skipped := unmanaged[key]; skipped {
			fmt.Fprintf(os.Stderr, "⚠️  Skipping unmanaged %s; use --force to manage it again\n", key)
			continue
		}
		out = append(out, t)
	}
	return out
}

// prepareRunTransaction initializes the transaction manager for a run. A dry
// run opens no manager, but it must not plan against a tree left half-written
// by an interrupted run.
func prepareRunTransaction(rc RunContext, rootPath string) (*transaction.Manager, func(), error) {
	if rc.DryRun {
		if err := transaction.CheckPending(rootPath); err != nil {
			return nil, nil, err
		}
	}
	return setupTransaction(rootPath, rc.DryRun, rc.NewProject)
}

// setupTransaction initializes the transaction safety net and returns a cleanup rollback deferral.
// Handled errors and panics on the main goroutine trigger rollback of tracked changes; external
// command side effects may remain, and crashes bypass deferred cleanup.
// Calling tx.Commit() explicitly at the end disables the rollback safety net.
func setupTransaction(rootPath string, dryRun, removeRootOnRecovery bool) (*transaction.Manager, func(), error) {
	if dryRun {
		return nil, func() {}, nil
	}

	tx, err := transaction.NewWithOptions(rootPath, transaction.Options{
		RemoveRootOnRecovery: removeRootOnRecovery,
	})
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

// taskContextWithTimeout derives the per-run timeout from the caller's signal
// context. A nil RunContext.Ctx means a background context, preserving the
// library behavior for callers that do not supply one.
func taskContextWithTimeout(rc RunContext) (context.Context, context.CancelFunc) {
	parent := rc.Ctx
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, defaultTimeout)
}

// buildTaskContext constructs future manifest state and validates template schemas against metadata.
func buildTaskContext(
	rc RunContext,
	m *types.Manifest,
	changes *manifestChanges,
	tx *transaction.Manager,
	rootPath string,
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

	applyManifestChanges(futureManifest, changes)

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

	// Prepare task context with default timeout, derived from the caller's
	// context so SIGINT/SIGTERM cancellation reaches exec.CommandContext and
	// the executor's cancellation checks.
	ctx, cancel := taskContextWithTimeout(rc)
	tc := types.TaskContext{
		Ctx:              ctx,
		DryRun:           rc.DryRun,
		Update:           rc.Update,
		Manifest:         futureManifest,
		TargetPath:       rc.TargetPath,
		Root:             rootPath,
		Force:            rc.Force,
		ConflictStrategy: rc.ConflictStrategy,
		TemplateDir:      rc.TemplateDir,
		RegistryCacheDir: rc.RegistryCacheDir,
		RegistryNames:    rc.RegistryNames,
		Tx:               tx,
		PlannedWrites:    rc.DryRunWrites,
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
	// Persist the manifest before the version-control band so the initial Git
	// commit captures the final manifest instead of leaving the repository
	// dirty. Version-control tasks are explicitly last, so nothing after them
	// changes tracked files.
	if err := executePlan(plan, tc, reporter, int(types.PrioVersionControl), func() error {
		// A signal can arrive after the last task returns. Do not persist or
		// commit a run that was canceled while it still had work left.
		if err := contextError(tc.Ctx); err != nil {
			return fmt.Errorf("execution interrupted before updating manifest: %w", err)
		}

		reporter.Step("✍️", "Updating manifest...")
		return persistManifestState(m, tc, changes, rootPath, tx)
	}); err != nil {
		return err
	}

	if err := contextError(tc.Ctx); err != nil {
		return fmt.Errorf("execution interrupted before commit: %w", err)
	}

	reporter.Step("✅", "Committing transaction...")
	// Point of No Return: We delete the backups.
	if err := tx.Commit(); err != nil {
		if errors.Is(err, transaction.ErrCommitted) {
			// The committed record is durable. A cleanup failure cannot undo
			// the scaffold, so report it without failing the command.
			fmt.Fprintf(os.Stderr, "⚠️  Transaction committed with warning: %v\n", err)
			return nil
		}
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

	persistManifestMetadata(m, tc)
	persistManagedFiles(m, tc)
	persistAcceptedFiles(m, tc)
	persistUnmanagedFiles(m, tc)

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

	snapshotManifestContent(tx, manifestPath)
	return nil
}

// persistManifestMetadata copies the context's metadata into the manifest.
func persistManifestMetadata(m *types.Manifest, tc types.TaskContext) {
	if tc.Manifest.Metadata == nil {
		return
	}
	if m.Metadata == nil {
		m.Metadata = make(map[string]string)
	}
	for k, v := range tc.Manifest.Metadata {
		m.Metadata[k] = v
	}
}

// persistManagedFiles copies the context's managed-file state into the
// manifest.
func persistManagedFiles(m *types.Manifest, tc types.TaskContext) {
	if tc.Manifest.ManagedFiles == nil {
		return
	}
	if m.ManagedFiles == nil {
		m.ManagedFiles = make(map[string]string)
	}
	for k, v := range tc.Manifest.ManagedFiles {
		m.ManagedFiles[k] = v
	}
}

// persistAcceptedFiles replaces the manifest's accepted_files map with the
// context state instead of merging. Replace semantics matter because a
// canonical write clears acceptance; a merge would resurrect the old entry.
func persistAcceptedFiles(m *types.Manifest, tc types.TaskContext) {
	if tc.Manifest == nil {
		return
	}
	if len(tc.Manifest.AcceptedFiles) == 0 {
		m.AcceptedFiles = nil
		return
	}
	accepted := make(map[string]string, len(tc.Manifest.AcceptedFiles))
	for k, v := range tc.Manifest.AcceptedFiles {
		accepted[k] = v
	}
	m.AcceptedFiles = accepted
}

// persistUnmanagedFiles replaces the manifest's unmanaged_files list with the
// sorted, deduplicated context state. Replace semantics let an apply --force
// that re-manages a key remove it from the list durably.
func persistUnmanagedFiles(m *types.Manifest, tc types.TaskContext) {
	if tc.Manifest == nil {
		return
	}
	m.UnmanagedFiles = tasks.NormalizeUnmanagedFiles(tc.Manifest.UnmanagedFiles)
}

// snapshotManifestContent records a newly created manifest's bytes so rollback
// removes it only when they are unchanged. Best-effort: a failure keeps the
// manifest on rollback rather than risking unverified content.
func snapshotManifestContent(tx *transaction.Manager, manifestPath string) {
	if err := tx.SnapshotCreatedContent(manifestPath); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Could not snapshot manifest content for rollback: %v\n", err)
	}
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
	applyManifestChanges(m, changes)
	return nil
}

// applyManifestChanges merges planned projects and templates into a manifest,
// skipping entries that already exist. It is shared by the render-time manifest
// (buildTaskContext) and the persisted one (updateManifest) so that re-running
// update, or a second apply, never duplicates entries. Duplicating them would
// make templates that range .Projects render every project block twice.
func applyManifestChanges(m *types.Manifest, changes *manifestChanges) {
	existingProjects := make(map[string]bool, len(m.Projects))
	for _, proj := range m.Projects {
		existingProjects[proj.Path] = true
	}
	for _, newProj := range changes.Projects {
		if !existingProjects[newProj.Path] {
			m.Projects = append(m.Projects, newProj)
			existingProjects[newProj.Path] = true
		}
	}

	existingTemplates := make(map[string]bool, len(m.Templates))
	for _, tmpl := range m.Templates {
		existingTemplates[tmpl.Name+":"+tmpl.Path] = true
	}
	for _, newTmpl := range changes.Templates {
		key := newTmpl.Name + ":" + newTmpl.Path
		if !existingTemplates[key] {
			m.Templates = append(m.Templates, newTmpl)
			existingTemplates[key] = true
		}
	}
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

// repoRelativeProjectPath converts a path into a clean, slash-separated path
// relative to the manifest root. The argument is resolved against the process
// working directory first, so every accepted spelling of the same target yields
// one value and the manifest stays portable. A target outside rootPath is
// rejected: stored paths are root-relative and must never escape the root.
func repoRelativeProjectPath(rootPath, pathArg string) (string, error) {
	if pathArg == "" {
		return "", errors.New("project path must not be empty")
	}
	absPath, err := filepath.Abs(pathArg)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %q: %w", pathArg, err)
	}
	rel, err := filepath.Rel(rootPath, absPath)
	if err != nil {
		return "", fmt.Errorf("cannot make path %q relative to %q: %w", pathArg, rootPath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target %q is outside the manifest root %q", pathArg, rootPath)
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

// buildPlan constructs the list of tasks based on CLI flags. rootPath is the
// manifest root and projectPath is the canonical root-relative path of the
// target; every manifest entry records the latter.
func buildPlan(rc RunContext, rootPath, projectPath string) (*types.Plan, string, *manifestChanges, error) {
	plan := &types.Plan{Tasks: []types.Task{}}
	changes := &manifestChanges{}
	commitMessage := "scbake: Apply templates"
	didSomething := false

	if rc.LangFlag != "" {
		didSomething = true
		msg, err := handleLangFlag(rc, rootPath, projectPath, plan, changes)
		if err != nil {
			return nil, "", nil, err
		}
		commitMessage = msg
	}

	if len(rc.WithFlag) > 0 {
		didSomething = true
		if err := handleWithFlag(rc, projectPath, plan, changes); err != nil {
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

// CheckLangPrerequisites validates that required binaries are available for the language.
func CheckLangPrerequisites(langFlag string) error {
	switch langFlag {
	case "go":
		return preflight.CheckBinaries("go")
	case "svelte":
		return preflight.CheckBinaries("npm")
	case "spring":
		// Scaffolding only downloads and unpacks the starter archive. Java is
		// needed later to build the generated project, not to create it.
		return preflight.CheckBinaries("curl", "unzip")
	}
	return nil
}

// CheckGitPrerequisites validates the prerequisites for actually creating a
// repository in targetPath. It is used by `new` before the project directory
// exists and by the apply pipeline before any write. A target that the git
// template will skip (existing repository or nested work tree) needs no
// identity.
func CheckGitPrerequisites(targetPath string) error {
	if preflight.GitSkipReason(targetPath) != "" {
		return nil
	}
	return preflight.CheckGitIdentity(targetPath)
}

// preflightGit enforces the git template's execution prerequisites at plan
// time, before the transaction writes anything. A dry run fails on the same
// prerequisite: reporting success for a run that cannot commit would be a
// false preview (see #144).
func preflightGit(rc RunContext) error {
	if rc.Update || !containsTemplate(rc.WithFlag, gitTemplateName) {
		return nil
	}
	// One shared skip/identity decision with `new` validation and the handler.
	return CheckGitPrerequisites(rc.TargetPath)
}

// containsTemplate reports whether name appears in the template selection.
func containsTemplate(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

// projectNameSource returns the value a generated manifest's project name is
// derived from. For `new`, the manifest path stays "." while the directory
// being created supplies the name; a real run has changed into it, and a dry
// run plans against the relative project name. Otherwise the name comes from
// the resolved target, so a spelling such as "..", "./svc", or an absolute
// path yields the target directory's base name instead of the literal argument.
func projectNameSource(rc RunContext, rootPath, projectPath string) string {
	if rc.NewProject {
		return rc.TargetPath
	}
	if projectPath == "." {
		return rootPath
	}
	return projectPath
}

// templateTaskProvider is the part of the tooling handler interface that core
// needs to build a plan. Language handlers share this shape but additionally
// take explicit schema variables (see langTaskProvider).
type templateTaskProvider interface {
	GetTasks(targetPath, templateDir, registryCacheDir string) ([]types.Task, error)
}

// langTaskProvider is the part of the language handler interface that core
// needs to build a plan.
type langTaskProvider interface {
	GetTasks(targetPath, templateDir, registryCacheDir string, vars map[string]string) ([]types.Task, error)
}

// getTemplateTasks asks a tooling handler for its tasks, preferring the optional
// RegistryAwareHandler interface so handlers that resolve registry-cached
// templates at plan time receive the configured registry allowlist. Handlers
// that only build CreateTemplateTask resolve later, at execution time.
func getTemplateTasks(h templateTaskProvider, rc RunContext) ([]types.Task, error) {
	if ra, ok := h.(types.RegistryAwareHandler); ok {
		return ra.GetTasksWithRegistries(rc.TargetPath, rc.TemplateDir, rc.RegistryCacheDir, rc.RegistryNames)
	}
	return h.GetTasks(rc.TargetPath, rc.TemplateDir, rc.RegistryCacheDir)
}

// getLangTasks asks a language handler for its tasks, preferring the optional
// RegistryAwareHandler interface so handlers that resolve registry-cached
// templates at plan time receive the configured registry allowlist.
func getLangTasks(h langTaskProvider, rc RunContext) ([]types.Task, error) {
	if ra, ok := h.(types.RegistryAwareHandler); ok {
		return ra.GetTasksWithRegistries(rc.TargetPath, rc.TemplateDir, rc.RegistryCacheDir, rc.RegistryNames)
	}
	return h.GetTasks(rc.TargetPath, rc.TemplateDir, rc.RegistryCacheDir, rc.SetVars)
}

// handleLangFlag processes the --lang flag, adding language tasks and project
// info. rootPath is the manifest root and projectPath the canonical
// root-relative path of the target.
func handleLangFlag(rc RunContext, rootPath, projectPath string, plan *types.Plan, changes *manifestChanges) (string, error) {
	// `update` only writes managed files, so language toolchains (go, npm,
	// java) need not be installed. A dry run only warns, so the plan still prints.
	if !rc.Update {
		if err := CheckLangPrerequisites(rc.LangFlag); err != nil {
			if !rc.DryRun {
				return "", err
			}
			fmt.Fprintf(os.Stderr, "⚠️  %v\n", err)
		}
	}

	handler, err := lang.GetHandler(rc.LangFlag)
	if err != nil {
		return "", err
	}

	// vars is deliberately rc.SetVars (explicit --set only). Schema defaults are
	// applied later in buildTaskContext and must never drive command construction.
	// getLangTasks forwards them alongside the configured registry allowlist.
	langTasks, err := getLangTasks(handler, rc)
	if err != nil {
		return "", fmt.Errorf("failed to get tasks for lang '%s': %w", rc.LangFlag, err)
	}

	if len(langTasks) == 0 {
		return "", fmt.Errorf("language handler '%s' returned no tasks (possible misconfiguration)", rc.LangFlag)
	}

	stampTemplateName(langTasks, rc.LangFlag)
	plan.Tasks = append(plan.Tasks, langTasks...)

	// For `new`, the manifest's project path stays "." (it is relative to the
	// project root), but the project name must come from the directory being
	// created. A real run has changed into that directory, so its TargetPath is
	// "."; a dry run passes the relative project name instead. Otherwise the
	// name comes from the resolved target path. Deriving the name from the
	// resolved target in both cases keeps the preview and the generated manifest
	// in agreement and avoids recording a spelling such as "..".
	projectName, err := util.SanitizeModuleName(projectNameSource(rc, rootPath, projectPath))
	if err != nil {
		return "", fmt.Errorf("could not determine project name: %w", err)
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

// handleWithFlag processes the --with flag, adding template tasks. projectPath
// is the canonical root-relative path of the target.
func handleWithFlag(rc RunContext, projectPath string, plan *types.Plan, changes *manifestChanges) error {
	for _, tmplName := range rc.WithFlag {
		if tmplName == gitTemplateName {
			if reason := preflight.GitSkipReason(rc.TargetPath); reason != "" {
				fmt.Fprintf(os.Stderr, "ℹ️  Skipping template '%s': %s (no repository will be initialized)\n", tmplName, reason)
				// Keep master's manifest behavior: record the requested template
				// even when it planned no tasks, using the canonical project
				// path so update selection matches like every other entry.
				// `update` filters side-effecting tasks, so this entry stays
				// inert.
				changes.Templates = append(changes.Templates, types.Template{
					Name: tmplName,
					Path: projectPath,
				})
				continue
			}
		}

		handler, err := templates.GetHandler(tmplName)
		if err != nil {
			return err
		}

		tmplTasks, err := getTemplateTasks(handler, rc)
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
			Path: projectPath,
		})
	}

	return nil
}
