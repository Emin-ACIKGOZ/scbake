// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/merge"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"sort"
	"strings"
	"text/template"
)

// templateFuncs provides utility functions for Go templates.
var templateFuncs = template.FuncMap{
	"default": func(defaultValue interface{}, given interface{}) interface{} {
		if given == nil {
			return defaultValue
		}
		switch v := given.(type) {
		case string:
			if v == "" {
				return defaultValue
			}
		case []interface{}:
			if len(v) == 0 {
				return defaultValue
			}
		}
		return given
	},
	"dict": func(values ...interface{}) (map[string]interface{}, error) {
		if len(values)%2 != 0 {
			return nil, errors.New("invalid dict call")
		}
		//nolint:mnd // Keys and values come in pairs
		dict := make(map[string]interface{}, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, ok := values[i].(string)
			if !ok {
				return nil, errors.New("dict keys must be strings")
			}
			dict[key] = values[i+1]
		}
		return dict, nil
	},
	"quote": func(s string) string {
		return fmt.Sprintf("%q", s)
	},
}

// templateRenderData is the data passed to a template when no explicit
// TemplateData override is set. Embedding the manifest keeps every legacy
// template field (.Projects, .Metadata, .SbakeVersion, ...) available, while
// Project names the manifest project that owns the task's target directory.
//
// Project is an additive render field, not a manifest field. It is nil when no
// [[projects]] entry matches the task's target directory (for example, a
// monorepo root when no project declares the root path), or when execution has
// no transaction root. A root project with path "." resolves normally.
// Templates must guard it and fall back intentionally:
//
//	{{ if .Project }}{{ .Project.Name }}{{ else }}fallback{{ end }}
type templateRenderData struct {
	*types.Manifest
	Project *types.Project
}

// CreateTemplateTask renders and writes a file from an embedded template.
type CreateTemplateTask struct {
	// TemplateFS is the embedded filesystem (e.g., lang.GoTemplates)
	TemplateFS embed.FS

	// TemplatePath is the path *within* the embed.FS (e.g., "go.mod.tpl")
	TemplatePath string

	// OutputPath is the destination path relative to the TargetPath (e.g., "go.mod")
	OutputPath string

	Desc string

	TaskPrio int

	// Optional: Custom data to pass to the template instead of the default
	// render data (manifest plus .Project).
	TemplateData interface{}

	// TemplateName identifies which registry template this task renders. It is
	// threaded to ReadTemplate so registry cache resolution is constrained to
	// the requested template and never reads a sibling template's file.
	TemplateName string

	// dryRunNote is the read-only reconciliation note recorded by Validate and
	// surfaced through DryRunNote. It is empty for an ordinary write.
	dryRunNote string
}

// Description implements types.Task.
func (t *CreateTemplateTask) Description() string {
	return t.Desc
}

// DescriptionWithSource implements types.SourceDescriber. It appends the tier
// that would supply this template so dry-run output discloses whether the file
// comes from --template-dir, a configured registry, or the embedded defaults.
// It uses the same resolver as Execute and is only called for dry-run display.
func (t *CreateTemplateTask) DescriptionWithSource(tc types.TaskContext) string {
	src := ResolveTemplateSource(t.TemplateFS, t.TemplatePath, tc.TemplateDir, tc.RegistryCacheDir, t.TemplateName, tc.RegistryNames)
	if src.Tier == "" {
		return t.Desc
	}
	return t.Desc + " [" + src.Label() + "]"
}

// Priority implements types.Task.
func (t *CreateTemplateTask) Priority() int {
	return t.TaskPrio
}

// WritesManagedFile marks this task as safe to replay during update/repair: it
// reconciles a path recorded in the managed_files ledger.
func (t *CreateTemplateTask) WritesManagedFile() bool { return true }

// ManagedOutputPath returns the target-relative path this task writes. The plan
// filter joins it with the project's root-relative path to compare against
// unmanaged_files before any task runs, so dry-run and the real run agree.
func (t *CreateTemplateTask) ManagedOutputPath() string { return t.OutputPath }

// HashContent calculates the SHA-256 hash of the given bytes after normalizing
// text line endings. CRLF and LF therefore hash identically, which keeps the
// managed_files ledger stable across Git checkouts that rewrite EOL bytes.
// NUL-free content is normalized; NUL-bearing binary content is hashed
// byte-exact.
func HashContent(content []byte) string {
	return rawContentHash(normalizeEOL(content))
}

// rawContentHash returns the byte-exact SHA-256 of content without EOL
// normalization.
func rawContentHash(content []byte) string {
	hasher := sha256.New()
	hasher.Write(content)
	return hex.EncodeToString(hasher.Sum(nil))
}

// BaseMatchesRecordedHash reports whether base is the pristine content recorded
// by recorded in the managed_files ledger. It accepts the EOL-insensitive hash
// and, for manifests written before that contract, the byte-exact raw hash. An
// empty recorded hash never matches, so a malformed ledger entry cannot make
// arbitrary base content look valid.
//
// The write path and read-only drift detection share this predicate so a base
// accepted by `update` is never reported as invalid by `drift`, and vice versa.
func BaseMatchesRecordedHash(base []byte, recorded string) bool {
	if recorded == "" {
		return false
	}
	return HashContent(base) == recorded || rawContentHash(base) == recorded
}

// normalizeEOL returns content with every run of one or more CR bytes that is
// immediately followed by LF collapsed to a single LF. This is a single-pass
// projection, so it is idempotent even for pathological CR runs: "\r\r\n"
// becomes "\n" on the first pass and cannot change again. Lone CR bytes and
// CR runs not followed by LF are preserved as content.
//
// This is the EOL-insensitive text-hash contract: line endings are not project
// content, so a working file rewritten by a Windows editor still matches the
// recorded pristine render. Any content containing a NUL byte is treated as
// binary and returned unchanged, so binary hashes remain byte-exact.
func normalizeEOL(content []byte) []byte {
	if bytes.IndexByte(content, 0) >= 0 {
		return content
	}
	if !bytes.Contains(content, []byte("\r\n")) {
		return content
	}
	out := make([]byte, 0, len(content))
	for i := 0; i < len(content); {
		if content[i] != '\r' {
			out = append(out, content[i])
			i++
			continue
		}
		end := i
		for end < len(content) && content[end] == '\r' {
			end++
		}
		if end < len(content) && content[end] == '\n' {
			out = append(out, '\n')
			i = end + 1
			continue
		}
		out = append(out, content[i:end]...)
		i = end
	}
	return out
}

// checkReconciliation evaluates drift and conflict strategies.
//
// It returns the path to write to (empty to skip), an optional replacement for
// the pending render (used by the merge strategy), and an error if the
// operation should abort.
//
// outputRelPath is the target-relative path used to read the file; ledgerKey is
// the project-scoped managed_files key used for state lookup; newContent is the
// pristine render the template is trying to write.
func checkReconciliation(
	absFinalPath, outputRelPath, ledgerKey, newContentHash string,
	newContent []byte,
	tc types.TaskContext,
) (string, []byte, error) {
	// If force is enabled, we always overwrite the original file, ignoring state.
	if tc.Force {
		return absFinalPath, nil, nil
	}

	// Read existing file
	existingContent, err := fileutil.SafeReadFile(tc.TargetPath, outputRelPath)
	if err != nil {
		if os.IsNotExist(err) {
			return absFinalPath, nil, nil // Safe to write, file doesn't exist
		}
		return "", nil, fmt.Errorf("failed to read existing file %s: %w", outputRelPath, err)
	}

	existingHash := HashContent(existingContent)

	// Check state
	originalHash := recordedHash(tc.Manifest, ledgerKey, outputRelPath, existingHash)

	// If the file exists but we've never managed it (or didn't record it), it's a conflict
	if originalHash == "" {
		p, cErr := handleConflict(absFinalPath, outputRelPath, tc.ConflictStrategy)
		return p, nil, cErr
	}

	// An accepted baseline is a local working-tree baseline for the current
	// template render. It only suppresses a rewrite while the worktree still
	// equals the accepted hash and the template still renders the pristine
	// hash. A later edit (existingHash != accepted) or a template change
	// (newContentHash != originalHash) falls through to normal conflict
	// handling, and overwrite always writes.
	if acceptedBaselineSuppressesWrite(tc, ledgerKey, existingHash, newContentHash, originalHash) {
		return "", nil, nil
	}

	// The template has not changed since we recorded this file. For the merge
	// strategy there is nothing to apply regardless of local edits, which is
	// what makes a second `update` a no-op. Other strategies keep the historic
	// behavior of surfacing drift.
	if unchangedTemplateForMerge(tc, newContentHash, originalHash) {
		return "", nil, nil
	}

	// The file still matches what scbake last wrote, so it has no local drift
	// and the new template content can be written directly.
	if existingHash == originalHash {
		return absFinalPath, nil, nil
	}

	// The on-disk file already equals the pending render: converged.
	if existingHash == newContentHash {
		return absFinalPath, nil, nil
	}

	// Both the template and the file changed. Merge when asked and a pristine
	// base is available; otherwise fall back to an artifact so local edits are
	// never clobbered.
	if tc.ConflictStrategy == types.ConflictMerge {
		return reconcileMerge(absFinalPath, outputRelPath, ledgerKey, originalHash, existingContent, newContent, tc)
	}

	p, cErr := handleConflict(absFinalPath, outputRelPath, tc.ConflictStrategy)
	return p, nil, cErr
}

// unchangedTemplateForMerge reports whether the merge strategy can skip this
// file because the template still renders the recorded pristine hash.
func unchangedTemplateForMerge(tc types.TaskContext, newContentHash, originalHash string) bool {
	return newContentHash == originalHash && tc.ConflictStrategy == types.ConflictMerge
}

// acceptedBaselineSuppressesWrite reports whether an accepted working-tree
// baseline makes this write a no-op. Without the existingHash condition a
// second edit after accept would be silently no-op'd.
func acceptedBaselineSuppressesWrite(
	tc types.TaskContext, ledgerKey, existingHash, newContentHash, originalHash string,
) bool {
	if tc.Manifest == nil {
		return false
	}
	accepted := tc.Manifest.AcceptedFiles[ledgerKey]
	return accepted != "" && existingHash == accepted && newContentHash == originalHash &&
		tc.ConflictStrategy != types.ConflictOverwrite
}

// reconcileMerge performs the `merge` strategy: a clean three-way merge writes
// the merged content, anything else writes an artifact so no local edit is lost.
func reconcileMerge(
	absFinalPath, outputRelPath, ledgerKey, originalHash string,
	existingContent, newContent []byte,
	tc types.TaskContext,
) (string, []byte, error) {
	base, ok := readBaseShadow(tc, ledgerKey)
	if !ok {
		fmt.Fprintf(os.Stderr,
			"⚠️  No valid pristine base for %s (missing); writing new template to artifact.\n",
			outputRelPath)
		return absFinalPath + ".scbake-new", nil, nil
	}
	// The base must be the pristine render recorded in the ledger. A stale or
	// corrupt base is not a valid common ancestor: the merge can silently keep
	// one side and drop the other. Treat it as missing and preserve the local
	// file via the artifact path.
	//
	// Legacy migration: binaries before the EOL-insensitive contract recorded
	// the raw SHA-256 of the rendered bytes. A migrated CRLF base therefore
	// matches originalHash only byte-exactly, not through HashContent. Accept
	// either form; a truly stale base still matches neither.
	if !BaseMatchesRecordedHash(base, originalHash) {
		fmt.Fprintf(os.Stderr,
			"⚠️  No valid pristine base for %s (hash mismatch); writing new template to artifact.\n",
			outputRelPath)
		return absFinalPath + ".scbake-new", nil, nil
	}
	// Merge EOL-normalized inputs so a whole-file CRLF rewrite does not look
	// like every line changed. The base tree itself keeps the pristine bytes.
	if merged, status := merge.Merge(
		normalizeEOL(base), normalizeEOL(existingContent), normalizeEOL(newContent),
	); status == merge.Clean {
		return absFinalPath, merged, nil
	}
	p, cErr := handleConflict(absFinalPath, outputRelPath, types.ConflictMerge)
	return p, nil, cErr
}

func handleConflict(absFinalPath, outputRelPath, strategy string) (string, error) {
	switch strategy {
	case types.ConflictOverwrite:
		return absFinalPath, nil
	case types.ConflictArtifact, types.ConflictMerge:
		fmt.Fprintf(os.Stderr, "⚠️  Conflict in %s (user modifications detected). Writing new template to artifact.\n", outputRelPath)
		return absFinalPath + ".scbake-new", nil
	case types.ConflictKeepLocal:
		fmt.Fprintf(os.Stderr, "⚠️  Conflict in %s (user modifications detected). Skipping update (--strategy=keep-local).\n", outputRelPath)
		return "", nil // Signal to skip
	case types.ConflictFail:
		fallthrough
	default:
		return "", fmt.Errorf("file %s has manual modifications (drift detected). Use --conflict-strategy to resolve", outputRelPath)
	}
}

// checkFilePreconditions validates that the output path stays inside the target
// directory and ensures its parent directory exists. It returns the resolved
// absolute output path.
func checkFilePreconditions(tc types.TaskContext, output, target string) (string, error) {
	absPath, err := resolveTaskPath(target, output, "output")
	if err != nil {
		return "", err
	}

	// A dry run still resolves and validates the path, but must not register
	// implicit parents or create anything. The executor never calls Execute in
	// a dry run; this guard protects direct and library callers.
	if tc.DryRun {
		return absPath, nil
	}

	// Register implicit parents for rollback (see trackMissingAncestors).
	if err := trackMissingAncestors(tc, absPath); err != nil {
		return "", err
	}

	// Ensure the directory exists
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return absPath, nil
}

// maybeAppendInternalStateIgnore adds the .scbake ignore and .scbake-base
// re-include block only when the managed output's ledger key is the
// manifest-root .gitignore. The key is root-relative, so equivalent paths
// normalize to ".gitignore", while a subproject's ".gitignore" is keyed like
// "sub/.gitignore" and must not inherit the root's state entries.
func maybeAppendInternalStateIgnore(ledgerKey string, content []byte) []byte {
	if ledgerKey != fileutil.GitIgnore {
		return content
	}
	return appendInternalStateIgnore(content)
}

// managedFileDecision is the read-only outcome of reconciling a proposed render
// against the ledger and the on-disk state. It is what WriteManagedFile would
// do, computed without touching the filesystem or the manifest.
type managedFileDecision struct {
	ledgerKey string
	content   []byte
	newHash   string
	writePath string
	override  []byte
}

// decideManagedFile computes what WriteManagedFile would do: ledger keying,
// internal-state ignore injection, hashing, and the conflict/merge decision. It
// is the single source of truth shared by Validate and WriteManagedFile, so a
// preview cannot disagree with the real run.
func decideManagedFile(tc types.TaskContext, absPath, outputRelPath string, content []byte) (managedFileDecision, error) {
	ledgerKey := managedFilesKey(tc, absPath, outputRelPath)

	// A plan-level filter normally removes unmanaged tasks, but direct and
	// library callers still need the execution-level defense. Returning an
	// empty write path is the same no-op signal as the other skip paths.
	if shouldSkipUnmanaged(tc, ledgerKey) {
		return managedFileDecision{ledgerKey: ledgerKey}, nil
	}

	// Make the ignore entry part of the managed render before hashing so the
	// ledger and base shadow match what is actually written.
	content = maybeAppendInternalStateIgnore(ledgerKey, content)

	newHash := HashContent(content)

	writePath, override, err := checkReconciliation(absPath, outputRelPath, ledgerKey, newHash, content, tc)
	if err != nil {
		return managedFileDecision{}, err
	}
	return managedFileDecision{
		ledgerKey: ledgerKey,
		content:   content,
		newHash:   newHash,
		writePath: writePath,
		override:  override,
	}, nil
}

// ValidateManagedFile reports whether a managed-file write would fail before
// writing, using the same resolution and reconciliation decision as
// WriteManagedFile. It is read-only: it never creates directories, tracks
// paths, writes files, or repairs the pristine base shadow.
//
// Tasks outside this package that own a managed file (for example the
// compliance LICENSE task) call it so their Validate matches the real run.
func ValidateManagedFile(tc types.TaskContext, outputRelPath string, content []byte) error {
	absPath, err := resolveTaskPath(tc.TargetPath, outputRelPath, "output")
	if err != nil {
		return err
	}
	_, err = decideManagedFile(tc, absPath, outputRelPath, content)
	return err
}

// WriteManagedFile performs state-aware reconciliation, transaction tracking,
// safe file writing, and manifest state updating for a generated file.
func WriteManagedFile(tc types.TaskContext, outputRelPath string, content []byte, perm os.FileMode) error {
	// A dry run must not create the parent directory. ValidateManagedFile is
	// the exact read-only half of this function.
	if tc.DryRun {
		return ValidateManagedFile(tc, outputRelPath, content)
	}

	// Resolve the ledger key before preconditions so an unmanaged path is
	// skipped without creating directories or walking conflict handling.
	_, skipped, err := manageWriteKey(tc, outputRelPath)
	if err != nil {
		return err
	}
	if skipped {
		return nil
	}

	absPath, err := checkFilePreconditions(tc, outputRelPath, tc.TargetPath)
	if err != nil {
		return err
	}

	decision, err := decideManagedFile(tc, absPath, outputRelPath, content)
	if err != nil {
		return err // Conflict strategy is 'fail'
	}
	ledgerKey, content, newHash := decision.ledgerKey, decision.content, decision.newHash
	writePath, override := decision.writePath, decision.override
	if writePath == "" {
		return nil // Skip (keep-local, unchanged, or a no-op)
	}

	// Idempotent artifact: a repeated conflict must not rewrite a side artifact
	// whose content is already identical.
	if isIdenticalArtifact(writePath, absPath, content) {
		return nil
	}

	if err := writePendingContent(tc, writePath, content, override, perm); err != nil {
		return err
	}

	if writePath == absPath {
		return recordCanonicalWrite(tc, ledgerKey, newHash, content)
	}
	return nil
}

// writePendingContent writes the pending render, preferring the merge override
// when reconciliation produced one.
func writePendingContent(
	tc types.TaskContext, writePath string, content, override []byte, perm os.FileMode,
) error {
	toWrite := content
	if override != nil {
		toWrite = override
	}
	return trackAndWrite(tc, writePath, toWrite, perm)
}

// manageWriteKey resolves the ledger key before preconditions so an unmanaged
// path is skipped without creating directories or walking conflict handling.
func manageWriteKey(tc types.TaskContext, outputRelPath string) (string, bool, error) {
	prelimAbs, err := fileutil.SafePath(tc.TargetPath, outputRelPath)
	if err != nil {
		return "", false, err
	}
	ledgerKey := managedFilesKey(tc, prelimAbs, outputRelPath)
	return ledgerKey, shouldSkipUnmanaged(tc, ledgerKey), nil
}

// shouldSkipUnmanaged reports whether the path is detached and this run must not
// re-manage it. update/repair never re-manage; a non-update run (apply) only
// re-manages when --force is set. The plan-level filter normally removes the
// task, so this is the execution-level defense.
func shouldSkipUnmanaged(tc types.TaskContext, ledgerKey string) bool {
	if !manifestListsUnmanaged(tc.Manifest, ledgerKey) || (!tc.Update && tc.Force) {
		return false
	}
	if !tc.Update {
		fmt.Fprintf(os.Stderr, "⚠️  Skipping unmanaged %s; use --force to manage it again\n", ledgerKey)
	}
	return true
}

// isIdenticalArtifact reports whether a non-canonical write already holds the
// pending bytes, so a repeated conflict does not rewrite the side artifact.
func isIdenticalArtifact(writePath, absPath string, content []byte) bool {
	return writePath != absPath && sameFileContent(writePath, content)
}

// recordCanonicalWrite advances the ledger entry and the pristine base shadow
// after a canonical write. Under artifact or keep-local the original file keeps
// its previous baseline (SCB-056); the recorded hash is always the pristine
// render, never the possibly-merged content, so a merged file stays recognized
// as locally modified.
func recordCanonicalWrite(tc types.TaskContext, ledgerKey, newHash string, content []byte) error {
	recordManagedHash(tc.Manifest, ledgerKey, newHash)
	// A canonical write advances the pristine baseline, so any accepted
	// working-tree baseline for this key is stale and must be cleared.
	unrecordAccepted(tc.Manifest, ledgerKey)
	// An explicit apply --force is the documented re-manage path: the canonical
	// write records the hash, so the key is no longer unmanaged. update/repair
	// never reach here for an unmanaged key.
	if !tc.Update && tc.Force {
		unrecordUnmanaged(tc.Manifest, ledgerKey)
	}
	return writeBaseShadow(tc, ledgerKey, content)
}

// ManagedFileUnchanged reports whether outputRelPath exists and still matches
// the hash recorded in the managed_files ledger. It is read-only: it creates no
// directories and performs no reconciliation. Callers use it to avoid
// re-fetching remote content during update/repair when the managed output is
// already current.
func ManagedFileUnchanged(tc types.TaskContext, outputRelPath string) (bool, error) {
	if tc.Manifest == nil {
		return false, nil
	}
	existing, err := fileutil.SafeReadFile(tc.TargetPath, outputRelPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	absPath, err := fileutil.SafePath(tc.TargetPath, outputRelPath)
	if err != nil {
		return false, err
	}
	existingHash := HashContent(existing)
	ledgerKey := managedFilesKey(tc, absPath, outputRelPath)
	recorded := recordedHash(tc.Manifest, ledgerKey, outputRelPath, existingHash)
	return recorded != "" && existingHash == recorded, nil
}

// EnsureManagedFileBase backfills the pristine base shadow for a managed file
// that still matches its recorded hash, when the base is missing or does not
// match. It uses the current file bytes, so it does not need the original
// source. It reports whether a base write was needed.
func EnsureManagedFileBase(tc types.TaskContext, outputRelPath string) (bool, error) {
	if tc.Manifest == nil {
		return false, nil
	}
	existing, err := fileutil.SafeReadFile(tc.TargetPath, outputRelPath)
	if err != nil {
		return false, err
	}
	absPath, err := fileutil.SafePath(tc.TargetPath, outputRelPath)
	if err != nil {
		return false, err
	}
	existingHash := HashContent(existing)
	ledgerKey := managedFilesKey(tc, absPath, outputRelPath)
	recorded := recordedHash(tc.Manifest, ledgerKey, outputRelPath, existingHash)
	if recorded == "" || existingHash != recorded {
		return false, nil
	}
	if base, ok := readBaseShadow(tc, ledgerKey); ok && BaseMatchesRecordedHash(base, recorded) {
		return false, nil
	}
	if err := writeBaseShadow(tc, ledgerKey, existing); err != nil {
		return false, err
	}
	return true, nil
}

// trackAndWrite registers path with the transaction and writes content to it
// through the symlink-safe helpers.
func trackAndWrite(tc types.TaskContext, path string, content []byte, perm os.FileMode) error {
	if tc.Tx != nil {
		if err := tc.Tx.TrackWrite(path, content); err != nil {
			return fmt.Errorf("failed to track file %s: %w", path, err)
		}
	}

	f, err := fileutil.SafeOpenFile(filepath.Dir(path), filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("failed to write to %s: %w", path, err)
	}
	return nil
}

// sameFileContent reports whether path already holds content byte-for-byte.
func sameFileContent(path string, content []byte) bool {
	existing, err := fileutil.SafeReadFile(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return false
	}
	return bytes.Equal(existing, content)
}

// rootAnchor returns the absolute project root that anchors root-relative
// decisions. The transaction root is authoritative when a transaction is
// active; a dry run has none, so TaskContext.Root supplies the same anchor. It
// reports false when neither is available, which preserves the legacy
// direct-caller behavior of the path helpers below.
func rootAnchor(tc types.TaskContext) (string, bool) {
	if tc.Tx != nil {
		if root := tc.Tx.Root(); root != "" {
			return root, true
		}
	}
	if tc.Root != "" {
		return filepath.Clean(tc.Root), true
	}
	return "", false
}

// taskRoot returns the absolute root a task's state lives under: the anchored
// project root when available, else the absolute target path.
func taskRoot(tc types.TaskContext) string {
	if root, ok := rootAnchor(tc); ok {
		return root
	}
	if tc.TargetPath == "" {
		return ""
	}
	abs, err := filepath.Abs(tc.TargetPath)
	if err != nil {
		return ""
	}
	return abs
}

// currentProject returns the manifest project that owns the current task's
// target directory, or nil when the owning project cannot be established.
//
// Project paths are recorded repo-relative while a task's TargetPath is the
// directory it writes into, so the lookup compares the target's path relative
// to the project root (the manifest root) with each project path. That requires
// an anchored root (a transaction root, or TaskContext.Root in a dry run):
// direct/library execution without one yields nil, and the template fallback is
// intentional there. It also yields nil when no [[projects]] entry matches the
// target directory (for example, a monorepo root when no project declares the
// root path); a root project with path "." resolves normally.
func currentProject(tc types.TaskContext) *types.Project {
	if tc.Manifest == nil || tc.TargetPath == "" {
		return nil
	}
	root, ok := rootAnchor(tc)
	if !ok {
		return nil
	}
	rel, ok := targetRelPath(root, tc.TargetPath)
	if !ok {
		return nil
	}
	return projectAtPath(tc.Manifest.Projects, rel)
}

// targetRelPath returns target's clean, slash-separated path relative to root.
// It reports false when either path cannot be resolved.
func targetRelPath(root, target string) (string, bool) {
	if !filepath.IsAbs(target) {
		abs, err := filepath.Abs(target)
		if err != nil {
			return "", false
		}
		target = abs
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(filepath.Clean(rel)), true
}

// projectAtPath returns the project whose declared path matches rel, or nil
// when no project claims that path. Empty paths never match.
func projectAtPath(projects []types.Project, rel string) *types.Project {
	for i := range projects {
		project := &projects[i]
		if project.Path == "" {
			continue
		}
		if filepath.ToSlash(filepath.Clean(project.Path)) == rel {
			return project
		}
	}
	return nil
}

// baseShadowKey maps a ledger key to its path inside the committed base tree.
// A base file must not keep the basename .gitignore or .gitattributes: Git
// applies those files to the base tree itself, which can hide sibling payloads
// or transform their bytes on checkout. The mapping applies at every depth so a
// nested managed .gitignore is sanitized too; other filenames are preserved.
func baseShadowKey(ledgerKey string) string {
	parts := strings.Split(ledgerKey, "/")
	for i, part := range parts {
		if part == fileutil.GitIgnore || part == fileutil.GitAttributes {
			parts[i] = part + fileutil.BaseShadowSuffix
		}
	}
	return strings.Join(parts, "/")
}

// BaseShadowRelPath returns the root-relative committed base path for a ledger
// key, using the same sanitization as the write path.
func BaseShadowRelPath(ledgerKey string) string {
	return filepath.Join(fileutil.CommittedBaseDir, filepath.FromSlash(baseShadowKey(ledgerKey)))
}

// baseShadowPath returns the committed base path for a ledger key.
func baseShadowPath(root, ledgerKey string) (string, error) {
	return fileutil.SafePath(root, BaseShadowRelPath(ledgerKey))
}

// trackBaseAncestors registers the not-yet-existing directories of a base
// shadow with the transaction, stopping at the project root. The committed base
// lives in .scbake-base outside .scbake, so a failed update must be able to
// remove it during rollback without ever registering the root itself.
func trackBaseAncestors(tc types.TaskContext, absPath string) error {
	if tc.Tx == nil || tc.DryRun {
		return nil
	}
	root := tc.Tx.Root()
	if root == "" {
		return nil
	}
	stop := root

	var missing []string
	for dir := filepath.Dir(absPath); dir != stop && isStrictlyWithin(stop, dir); dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil {
			break // First existing ancestor: everything above it already exists.
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to inspect base shadow ancestor %s: %w", dir, err)
		}
		missing = append(missing, dir)
	}
	// Register shallowest-first so rollback removes the deepest created
	// directory first, and only when empty; the project root is never
	// registered.
	for i := len(missing) - 1; i >= 0; i-- {
		if err := tc.Tx.Track(missing[i]); err != nil {
			return fmt.Errorf("failed to track base shadow ancestor %s: %w", missing[i], err)
		}
	}
	return nil
}

// writeBaseShadow stores the pristine rendered content of a managed file under
// <root>/.scbake-base/<ledgerKey>. `update` reads it back as the common ancestor
// for a three-way merge. The bytes are stored exactly as rendered; EOL
// normalization applies only to hashing and merge inputs. It is tracked by the
// transaction so a rollback removes or restores it alongside the file it mirrors.
func writeBaseShadow(tc types.TaskContext, ledgerKey string, content []byte) error {
	if tc.Tx == nil || tc.DryRun || ledgerKey == "" {
		return nil
	}
	root := tc.Tx.Root()
	if root == "" {
		return nil
	}
	absPath, err := baseShadowPath(root, ledgerKey)
	if err != nil {
		return fmt.Errorf("invalid base shadow path for %s: %w", ledgerKey, err)
	}
	if err := tc.Tx.TrackWrite(absPath, content); err != nil {
		return fmt.Errorf("failed to track base shadow %s: %w", absPath, err)
	}
	if err := trackBaseAncestors(tc, absPath); err != nil {
		return err
	}
	if err := writeBaseFile(absPath, content); err != nil {
		return err
	}
	return ensureInternalStateIgnored(tc, root)
}

// internalStateIgnoreEntry is the standalone .gitignore line that keeps the
// persistent .scbake state out of version control.
const internalStateIgnoreEntry = ".scbake/"

// committedBaseIgnoreEntries re-include the committed base tree. The directory
// line lets Git descend into .scbake-base; the glob re-includes every
// descendant, including dotfiles matched by patterns such as ".*" and payloads
// under hostile directories. The last line keeps the rules file itself
// stageable when the user's patterns include ".*" or "*" and .gitignore is not
// tracked yet.
//
// Order matters: the re-includes must remain at the end of the file. A later
// matching pattern such as "*" overrides them and can hide the base. The helper
// appends missing lines but deliberately does not move an existing block.
var committedBaseIgnoreEntries = []string{"!.scbake-base/", "!.scbake-base/**", "!.gitignore"}

// ensureInternalStateIgnored guarantees that <root>/.gitignore ignores the
// .scbake state directory and re-includes the committed .scbake-base tree,
// independently of the selected language pack. The base shadow is written for
// every generated project, so this closes the gap for tooling-only projects
// that have no Go language .gitignore template.
//
// It is idempotent with language-provided ignore entries and records the
// change through the transaction so rollback restores or removes the file.
func ensureInternalStateIgnored(tc types.TaskContext, root string) error {
	if tc.Tx == nil || tc.DryRun {
		return nil
	}

	relPath := fileutil.GitIgnore

	// A managed root .gitignore already carries the entry in its render (see
	// WriteManagedFile). Mutating it out-of-band here would leave the recorded
	// hash and base shadow stale, turning scbake's own append into user drift.
	// Verify it still provides the guarantee, but leave repair to apply/update.
	if tc.Manifest != nil && tc.Manifest.ManagedFiles[relPath] != "" {
		checkManagedGitignore(root, relPath)
		return nil
	}

	absPath, err := fileutil.SafePath(root, relPath)
	if err != nil {
		warnInternalStateIgnore(relPath, err)
		return nil
	}

	existing, err := readRootGitignore(root, relPath)
	if err != nil {
		warnInternalStateIgnore(relPath, err)
		return nil
	}
	if gitignoreIgnoresInternalState(existing) && gitignoreAllowsCommittedBase(existing) {
		return nil
	}

	return applyInternalStateIgnore(tc, root, relPath, absPath, existing)
}

// checkManagedGitignore warns when a managed root .gitignore no longer exists or
// no longer ignores the internal state directory and re-includes the committed
// base tree. It never mutates the file: the managed render owns the ledger and
// base shadow, so the fix is to re-run apply/update with overwrite enabled
// (--force with the same apply selector, or --conflict-strategy overwrite).
// Only a render that owns .gitignore (the language pack or a custom template)
// can regenerate it; tooling templates do not render the root .gitignore.
func checkManagedGitignore(root, relPath string) {
	existing, err := readRootGitignore(root, relPath)
	if err != nil {
		warnManagedGitignoreStale(relPath, "cannot be read: "+err.Error())
		return
	}
	if gitignoreIgnoresInternalState(existing) && gitignoreAllowsCommittedBase(existing) {
		return
	}
	warnManagedGitignoreStale(relPath,
		"is missing the "+fileutil.InternalDir+"/ ignore or the "+fileutil.CommittedBaseDir+"/ re-include")
}

// warnManagedGitignoreStale reports a managed .gitignore that no longer
// provides the internal-state ignore guarantee.
func warnManagedGitignoreStale(relPath, detail string) {
	fmt.Fprintf(
		os.Stderr,
		"⚠️  Managed %s %s; regenerate it with 'scbake apply' or 'scbake update' using --conflict-strategy overwrite (or --force with the same apply selector).\n",
		relPath, detail,
	)
}

// readRootGitignore returns the root .gitignore content. A missing file is not
// an error and yields nil content.
func readRootGitignore(root, relPath string) ([]byte, error) {
	existing, err := fileutil.SafeReadFile(root, relPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return existing, nil
}

// applyInternalStateIgnore appends the ignore entry through the transaction.
// It checks writability first so an existing read-only file does not get a
// backup that cannot be restored. Pre-write failures are best-effort and only
// warn; write failures propagate so the caller's transaction rolls back the
// possibly truncated file.
func applyInternalStateIgnore(tc types.TaskContext, root, relPath, absPath string, existing []byte) error {
	if err := checkIgnoredFileWritable(root, relPath); err != nil {
		warnInternalStateIgnore(relPath, err)
		return nil
	}
	return trackAndWrite(tc, absPath, appendInternalStateIgnore(existing), fileutil.FilePerms)
}

// checkIgnoredFileWritable fails early when an existing ignore file cannot be
// opened for writing, so the transaction does not record a backup it may be
// unable to restore later. A missing file is left to trackAndWrite to create.
func checkIgnoredFileWritable(root, relPath string) error {
	f, err := fileutil.SafeOpenFile(root, relPath, os.O_WRONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return f.Close()
}

// warnInternalStateIgnore reports a best-effort .gitignore update that was
// skipped, matching the warning style used for conflict handling.
func warnInternalStateIgnore(relPath string, err error) {
	fmt.Fprintf(os.Stderr, "⚠️  Could not update the scbake ignore rules in %s: %v\n", relPath, err)
}

// appendInternalStateIgnore returns content with the .scbake/ ignore and the
// .scbake-base/ re-include block appended when they are absent. Existing bytes
// are preserved exactly, and a separating newline is inserted only when the
// file does not already end with one.
func appendInternalStateIgnore(content []byte) []byte {
	out := content
	if !gitignoreIgnoresInternalState(out) {
		out = appendIgnoreLine(out, internalStateIgnoreEntry)
	}
	for _, entry := range committedBaseIgnoreEntries {
		if !gitignoreHasLine(out, entry) {
			out = appendIgnoreLine(out, entry)
		}
	}
	return out
}

// appendIgnoreLine appends one ignore line, inserting a separating newline only
// when the existing content does not already end with one.
func appendIgnoreLine(content []byte, entry string) []byte {
	out := content
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, entry...)
	return append(out, '\n')
}

// gitignoreIgnoresInternalState reports whether existing already ignores the
// .scbake state directory using any of its common spellings. Only trailing
// spaces, tabs, and CR are trimmed: Git treats leading whitespace as part of
// the pattern, so an indented entry does not ignore .scbake.
func gitignoreIgnoresInternalState(existing []byte) bool {
	for _, line := range strings.Split(string(existing), "\n") {
		switch strings.TrimRight(line, " \t\r") {
		case ".scbake", ".scbake/", "/.scbake", "/.scbake/":
			return true
		}
	}
	return false
}

// gitignoreAllowsCommittedBase reports whether existing carries every
// re-include line needed to stage new base files under broad ignore patterns.
func gitignoreAllowsCommittedBase(existing []byte) bool {
	for _, entry := range committedBaseIgnoreEntries {
		if !gitignoreHasLine(existing, entry) {
			return false
		}
	}
	return true
}

// gitignoreHasLine reports whether existing contains entry as a whole line.
func gitignoreHasLine(existing []byte, entry string) bool {
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimRight(line, " \t\r") == entry {
			return true
		}
	}
	return false
}

// writeBaseFile creates the base shadow's parent directory and writes content.
func writeBaseFile(absPath string, content []byte) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create base shadow directory %s: %w", dir, err)
	}
	f, err := fileutil.SafeOpenFile(dir, filepath.Base(absPath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileutil.PrivateFilePerms)
	if err != nil {
		return fmt.Errorf("failed to create base shadow %s: %w", absPath, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("failed to write base shadow %s: %w", absPath, err)
	}
	return nil
}

// readBaseShadow returns the pristine base content for a managed file, if it
// exists. The lookup uses the same sanitized mapping as the write path.
func readBaseShadow(tc types.TaskContext, ledgerKey string) ([]byte, bool) {
	if ledgerKey == "" {
		return nil, false
	}
	root := taskRoot(tc)
	if root == "" {
		return nil, false
	}
	data, err := fileutil.SafeReadFile(root, BaseShadowRelPath(ledgerKey))
	if err != nil {
		return nil, false
	}
	return data, true
}

// recordManagedHash stores the SHA-256 hash of a file scbake wrote at key in the
// manifest's managed_files ledger, initializing the map when necessary.
func recordManagedHash(m *types.Manifest, key, hash string) {
	if m.ManagedFiles == nil {
		m.ManagedFiles = make(map[string]string)
	}
	m.ManagedFiles[key] = hash
}

// manifestListsUnmanaged reports whether key is in the manifest's
// unmanaged_files list. A nil manifest never lists anything.
func manifestListsUnmanaged(m *types.Manifest, key string) bool {
	if m == nil || key == "" {
		return false
	}
	for _, unmanaged := range m.UnmanagedFiles {
		if unmanaged == key {
			return true
		}
	}
	return false
}

// NormalizeUnmanagedFiles drops empty keys and returns a sorted, deduplicated
// copy of keys. An empty result is nil so the field is omitted when saved.
func NormalizeUnmanagedFiles(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// unrecordAccepted removes a key from accepted_files. The map is left non-nil
// (an empty map is omitted by the TOML encoder).
func unrecordAccepted(m *types.Manifest, key string) {
	if m == nil || m.AcceptedFiles == nil {
		return
	}
	delete(m.AcceptedFiles, key)
}

// unrecordUnmanaged removes a key from unmanaged_files, preserving the sorted
// order of the remaining entries.
func unrecordUnmanaged(m *types.Manifest, key string) {
	if m == nil || len(m.UnmanagedFiles) == 0 {
		return
	}
	remaining := make([]string, 0, len(m.UnmanagedFiles))
	for _, unmanaged := range m.UnmanagedFiles {
		if unmanaged != key {
			remaining = append(remaining, unmanaged)
		}
	}
	m.UnmanagedFiles = NormalizeUnmanagedFiles(remaining)
}

// managedFilesKey derives the managed_files ledger key for a file: the path
// relative to the project root. Scoping by the root (rather than the target
// project) keeps monorepo subprojects that generate the same relative filename
// from aliasing each other's entry. Without an anchored root, or when the file
// lies outside it, it falls back to the target-relative path.
func managedFilesKey(tc types.TaskContext, absPath, outputRelPath string) string {
	root, ok := rootAnchor(tc)
	if !ok {
		return outputRelPath
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return outputRelPath
	}
	// Normalize to forward slashes so keys are identical across platforms and
	// match the slash-separated paths templates declare.
	return filepath.ToSlash(rel)
}

// recordedHash returns the hash scbake last recorded for a managed path. It
// looks up the project-scoped key first. For manifests written before keys were
// scoped, it falls back to the legacy project-relative key only when that hash
// matches the file's current on-disk content.
//
// This is the migration rule: an unchanged scbake-generated file is recognized
// and re-scoped. A mismatched legacy key (for example a stale entry left by
// another subproject) is not used as a drift baseline. Note the inherent limit:
// if an unmanaged file's bytes happen to equal a surviving legacy hash, the two
// cases are indistinguishable and the file is treated as managed.
func recordedHash(m *types.Manifest, ledgerKey, legacyKey, existingHash string) string {
	if m == nil || m.ManagedFiles == nil {
		return ""
	}
	if h := m.ManagedFiles[ledgerKey]; h != "" {
		return h
	}
	if ledgerKey == legacyKey {
		return ""
	}
	if legacy := m.ManagedFiles[legacyKey]; legacy != "" && legacy == existingHash {
		return legacy
	}
	return ""
}

// render produces the pristine bytes for this task's template.
func (t *CreateTemplateTask) render(tc types.TaskContext) ([]byte, error) {
	// 1. Read and parse the template using the override-aware helper
	tplContent, err := ReadTemplate(t.TemplateFS, t.TemplatePath, tc.TemplateDir, tc.RegistryCacheDir, t.TemplateName, tc.RegistryNames)
	if err != nil {
		return nil, fmt.Errorf("failed to read template %s: %w", t.TemplatePath, err)
	}

	tpl, err := template.New(t.TemplatePath).Funcs(templateFuncs).Parse(string(tplContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", t.TemplatePath, err)
	}

	// 2. Render template to memory buffer first
	data := interface{}(tc.Manifest)
	if tc.Manifest != nil {
		data = templateRenderData{Manifest: tc.Manifest, Project: currentProject(tc)}
	}
	if t.TemplateData != nil {
		data = t.TemplateData
	}

	var buf bytes.Buffer
	if err = tpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to render template %s: %w", t.TemplatePath, err)
	}
	return buf.Bytes(), nil
}

// DryRunNote implements types.DryRunAnnotator. Validate records the note from
// the same decision WriteManagedFile would use; an ordinary create or update
// records no note, leaving the executor's description unchanged.
func (t *CreateTemplateTask) DryRunNote() string { return t.dryRunNote }

// Validate resolves, renders, and reconciles the template without creating
// directories, tracking, writing, or mutating the ledger. It shares render and
// decideManagedFile with Execute, so the preview reports the real decision. It
// also records the dry-run annotation here because this is the only place that
// holds the shared reconciliation result.
func (t *CreateTemplateTask) Validate(tc types.TaskContext) error {
	t.dryRunNote = ""

	content, err := t.render(tc)
	if err != nil {
		return err
	}
	absPath, err := resolveTaskPath(tc.TargetPath, t.OutputPath, "output")
	if err != nil {
		return err
	}
	decision, err := decideManagedFile(tc, absPath, t.OutputPath, content)
	if err != nil {
		return err
	}
	t.dryRunNote = managedFileDryRunNote(absPath, decision)
	return nil
}

// managedFileDryRunNote describes a reconciliation that does not perform a
// plain create or update. An empty note means the ordinary write path, so a
// preview only gains text when the real run would skip, preserve, or divert
// the write.
func managedFileDryRunNote(absPath string, decision managedFileDecision) string {
	if decision.writePath == "" {
		return "no change"
	}
	if decision.writePath != absPath {
		return "conflict; would write artifact"
	}
	if decision.override != nil && sameFileContent(absPath, decision.override) {
		return "local edits preserved"
	}
	if sameFileContent(absPath, decision.content) {
		return "unchanged"
	}
	return ""
}

// Execute performs the template creation task.
func (t *CreateTemplateTask) Execute(tc types.TaskContext) error {
	content, err := t.render(tc)
	if err != nil {
		return err
	}
	// 3. Delegate state reconciliation, transaction tracking, and writing to WriteManagedFile
	return WriteManagedFile(tc, t.OutputPath, content, fileutil.FilePerms)
}
