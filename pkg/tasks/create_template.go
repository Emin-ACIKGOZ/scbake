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

	// Optional: Custom data to pass to the template instead of the full manifest
	TemplateData interface{}

	// TemplateName identifies which registry template this task renders. It is
	// threaded to ReadTemplate so registry cache resolution is constrained to
	// the requested template and never reads a sibling template's file.
	TemplateName string
}

// Description implements types.Task.
func (t *CreateTemplateTask) Description() string {
	return t.Desc
}

// Priority implements types.Task.
func (t *CreateTemplateTask) Priority() int {
	return t.TaskPrio
}

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

	// The template has not changed since we recorded this file. For the merge
	// strategy there is nothing to apply regardless of local edits, which is
	// what makes a second `update` a no-op. Other strategies keep the historic
	// behavior of surfacing drift.
	if newContentHash == originalHash && tc.ConflictStrategy == types.ConflictMerge {
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

// WriteManagedFile performs state-aware reconciliation, transaction tracking,
// safe file writing, and manifest state updating for a generated file.
func WriteManagedFile(tc types.TaskContext, outputRelPath string, content []byte, perm os.FileMode) error {
	absPath, err := checkFilePreconditions(tc, outputRelPath, tc.TargetPath)
	if err != nil {
		return err
	}
	ledgerKey := managedFilesKey(tc, absPath, outputRelPath)

	// Make the ignore entry part of the managed render before hashing so the
	// ledger and base shadow match what is actually written.
	content = maybeAppendInternalStateIgnore(ledgerKey, content)

	newHash := HashContent(content)

	writePath, override, err := checkReconciliation(absPath, outputRelPath, ledgerKey, newHash, content, tc)
	if err != nil {
		return err // Conflict strategy is 'fail'
	}
	if writePath == "" {
		return nil // Skip (keep-local, unchanged, or a no-op)
	}

	if tc.DryRun {
		return nil
	}

	// Idempotent artifact: a repeated conflict must not rewrite a side artifact
	// whose content is already identical.
	if writePath != absPath && sameFileContent(writePath, content) {
		return nil
	}

	toWrite := content
	if override != nil {
		toWrite = override
	}
	if err := trackAndWrite(tc, writePath, toWrite, perm); err != nil {
		return err
	}

	// The ledger entry (and the pristine base shadow that mirrors it) only
	// advances when we actually wrote the canonical path. Under artifact or
	// keep-local we write elsewhere (or nowhere), so the original file keeps its
	// previous baseline (SCB-056). The recorded hash is always the pristine
	// render, never the possibly-merged content, so a merged file stays
	// recognized as locally modified.
	if writePath == absPath {
		recordManagedHash(tc.Manifest, ledgerKey, newHash)
		return writeBaseShadow(tc, ledgerKey, content)
	}
	return nil
}

// trackAndWrite registers path with the transaction and writes content to it
// through the symlink-safe helpers.
func trackAndWrite(tc types.TaskContext, path string, content []byte, perm os.FileMode) error {
	if tc.Tx != nil {
		if err := tc.Tx.Track(path); err != nil {
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

// taskRoot returns the absolute root a task's state lives under: the
// transaction root when present, else the absolute target path.
func taskRoot(tc types.TaskContext) string {
	if tc.Tx != nil && tc.Tx.Root() != "" {
		return tc.Tx.Root()
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
	// Register shallowest-first so reverse-order rollback removes the deepest
	// first; RemoveAll on .scbake-base is safe and leaves the root intact.
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
	if err := tc.Tx.Track(absPath); err != nil {
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
// base shadow, so the fix is a normal apply/update that regenerates it.
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
		"⚠️  Managed %s %s; regenerate it with 'scbake apply' or 'scbake update'.\n",
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

// managedFilesKey derives the managed_files ledger key for a file: the path
// relative to the transaction's project root. Scoping by the root (rather than
// the target project) keeps monorepo subprojects that generate the same
// relative filename from aliasing each other's entry. Without a transaction
// manager, or when the file lies outside its root, it falls back to the
// target-relative path.
func managedFilesKey(tc types.TaskContext, absPath, outputRelPath string) string {
	if tc.Tx == nil {
		return outputRelPath
	}
	root := tc.Tx.Root()
	if root == "" {
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

// Execute performs the template creation task.
func (t *CreateTemplateTask) Execute(tc types.TaskContext) error {
	// 1. Read and parse the template using the override-aware helper
	tplContent, err := ReadTemplate(t.TemplateFS, t.TemplatePath, tc.TemplateDir, tc.RegistryCacheDir, t.TemplateName)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", t.TemplatePath, err)
	}

	tpl, err := template.New(t.TemplatePath).Funcs(templateFuncs).Parse(string(tplContent))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", t.TemplatePath, err)
	}

	// 2. Render template to memory buffer first
	data := interface{}(tc.Manifest)
	if t.TemplateData != nil {
		data = t.TemplateData
	}

	var buf bytes.Buffer
	if err = tpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to render template %s: %w", t.TemplatePath, err)
	}

	// 3. Delegate state reconciliation, transaction tracking, and writing to WriteManagedFile
	return WriteManagedFile(tc, t.OutputPath, buf.Bytes(), fileutil.FilePerms)
}
