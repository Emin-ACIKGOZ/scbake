// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/util"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
)

// RunAccept records the current working-tree bytes of one or more managed files
// as their accepted drift baseline. It never rewrites the working file, the
// pristine managed_files hash, or the committed base shadow: drift compares the
// working tree against the accepted hash while base validation stays pristine.
func RunAccept(rc RunContext, paths []string) error {
	return withRootLock(rc, func() error {
		return runManage(rc, paths, true)
	})
}

// RunUnmanage detaches one or more managed files from scbake: it removes the
// managed/accepted rows, records the root-relative key in unmanaged_files, and
// removes the committed base shadow transactionally. The working file is left
// untouched.
func RunUnmanage(rc RunContext, paths []string) error {
	return withRootLock(rc, func() error {
		return runManage(rc, paths, false)
	})
}

// manageAction is one validated path operation. The manifest is mutated on a
// deep copy while building actions, so a failure on any argument leaves the
// real manifest and every file untouched.
type manageAction struct {
	key    string
	accept bool

	// accept state
	hash            string
	cleared         bool
	alreadyCurrent  bool
	warnMissingBase bool

	// unmanage state
	alreadyUnmanaged bool
	baseRel          string
	baseAbs          string
	removeBase       bool
}

// runManage validates every argument before touching the project, then applies
// all changes in one transaction and one manifest save. Dry runs validate and
// print without opening a transaction, taking the root lock (withRootLock skips
// it), or writing anything.
func runManage(rc RunContext, paths []string, accept bool) error {
	m, root, err := loadManageManifest(rc)
	if err != nil {
		return err
	}

	// A dry run opens no transaction, so check for an interrupted run before
	// validating arguments: recovery is the actionable error even when a path
	// would also fail validation. Mutating runs are checked by
	// setupTransaction, and real no-ops are checked after planning below.
	if rc.DryRun {
		if err := transaction.CheckPending(root); err != nil {
			return err
		}
	}

	// Validate and plan against a copy. The real manifest is only replaced by
	// the copy when every argument succeeded.
	updated, actions, err := planManage(rc, root, paths, accept, m.DeepCopy())
	if err != nil {
		return err
	}
	changed := manageActionsChanged(actions)

	if rc.DryRun || !changed {
		// A real no-op never reaches setupTransaction, so check it here to agree
		// with --dry-run and drift instead of reporting success on a project
		// that needs recovery. The dry-run check above already ran.
		if !rc.DryRun {
			if err := transaction.CheckPending(root); err != nil {
				return err
			}
		}
		reportManageActions(actions, accept, rc.DryRun)
		reportManageWarnings(actions)
		return nil
	}

	return finishManage(applyManage(root, updated, actions), actions, accept)
}

// finishManage reports a completed apply. transaction.ErrCommitted means the
// transaction is durable but cleanup failed, so it is a successful run: report
// the cleanup warning along with the per-path confirmations and deferred
// warnings, then return nil. Any other error is returned before reporting
// because nothing is known to have landed.
func finishManage(applyErr error, actions []manageAction, accept bool) error {
	if applyErr != nil && !errors.Is(applyErr, transaction.ErrCommitted) {
		return applyErr
	}
	if applyErr != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Transaction committed with warning: %s\n",
			util.Sanitize(applyErr.Error()))
	}
	reportManageActions(actions, accept, false)
	reportManageWarnings(actions)
	return nil
}

// planManage validates every argument against a deep copy of the manifest and
// returns the updated copy plus one action per argument. Planning reads files
// and mutates only the copy, so a failure leaves the real manifest untouched.
func planManage(
	rc RunContext, root string, paths []string, accept bool, updated *types.Manifest,
) (*types.Manifest, []manageAction, error) {
	actions := make([]manageAction, 0, len(paths))
	for _, arg := range paths {
		key, err := resolveManagedKey(root, rc.TargetPath, arg)
		if err != nil {
			return nil, nil, err
		}

		var action manageAction
		if accept {
			action, err = planAccept(updated, root, key, arg)
		} else {
			action, err = planUnmanage(updated, root, key, arg)
		}
		if err != nil {
			return nil, nil, err
		}
		actions = append(actions, action)
	}
	return updated, actions, nil
}

// manageActionsChanged reports whether any planned action mutates persistent
// state.
func manageActionsChanged(actions []manageAction) bool {
	for _, action := range actions {
		if action.changed() {
			return true
		}
	}
	return false
}

// applyManage applies all planned actions in one transaction and one manifest
// save. It returns transaction.ErrCommitted when the commit is durable but
// cleanup failed, leaving the warning to the caller.
func applyManage(root string, updated *types.Manifest, actions []manageAction) error {
	tx, cleanup, err := setupTransaction(root, false, false)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := removeUnmanagedBases(tx, actions); err != nil {
		return err
	}

	// Save the manifest last so it is the commit point: a failure before this
	// rolls back the base removals, and a failure after it rolls back both.
	manifestPath := filepath.Join(root, fileutil.ManifestFileName)
	if err := tx.Track(manifestPath); err != nil {
		return fmt.Errorf("failed to track manifest file: %s", util.Sanitize(err.Error()))
	}
	if err := manifest.Save(updated, root); err != nil {
		return fmt.Errorf("manifest save failed: %s", util.Sanitize(err.Error()))
	}
	snapshotManifestContent(tx, manifestPath)

	if err := tx.Commit(); err != nil {
		if errors.Is(err, transaction.ErrCommitted) {
			return err
		}
		return fmt.Errorf("failed to commit transaction: %s", util.Sanitize(err.Error()))
	}
	return nil
}

// loadManageManifest loads the manifest and fails closed when scbake.toml is
// absent: accept/unmanage must never bootstrap a project.
func loadManageManifest(rc RunContext) (*types.Manifest, string, error) {
	m, root, err := manifest.LoadWithOptions(rc.TargetPath, manifest.DiscoveryOptions{
		AllowAncestorSearch: rc.AllowAncestorSearch,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to load %s: %s",
			fileutil.ManifestFileName, util.Sanitize(err.Error()))
	}

	manifestPath := filepath.Join(root, fileutil.ManifestFileName)
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("no %s found at %s",
				fileutil.ManifestFileName, util.Sanitize(root))
		}
		return nil, "", fmt.Errorf("failed to inspect %s: %s",
			util.Sanitize(manifestPath), util.Sanitize(err.Error()))
	}
	return m, root, nil
}

// resolveManagedKey turns an invocation-relative path into a root-relative,
// slash-separated managed key and rejects lexical traversal or symlink escape.
// The argument is resolved against the invocation directory, not the root, so a
// command run in a monorepo subproject addresses that subproject's file.
func resolveManagedKey(root, target, arg string) (string, error) {
	if arg == "" {
		return "", errors.New("path must not be empty")
	}
	abs := arg
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(target, arg)
	}
	abs = filepath.Clean(abs)

	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", fmt.Errorf("cannot make path %q relative to %q: %s",
			arg, root, util.Sanitize(err.Error()))
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the project root %s", arg, util.Sanitize(root))
	}

	key := filepath.ToSlash(filepath.Clean(rel))
	if _, err := fileutil.SafePath(root, filepath.FromSlash(key)); err != nil {
		return "", fmt.Errorf("path %q is outside the project root %s: %s",
			arg, util.Sanitize(root), util.Sanitize(err.Error()))
	}
	return key, nil
}

// listsUnmanagedKey reports whether key is in the manifest's unmanaged_files
// list.
func listsUnmanagedKey(m *types.Manifest, key string) bool {
	if m == nil {
		return false
	}
	for _, unmanaged := range m.UnmanagedFiles {
		if unmanaged == key {
			return true
		}
	}
	return false
}

// planAccept validates one accept argument and records the accepted hash on m.
// It reads the file once with the symlink-refusing reader and hashes those
// bytes. The committed base is preflighted against the pristine ledger hash: an
// invalid base fails so accept cannot claim success while drift stays red; a
// missing base only warns because missing bases are advisory by default.
func planAccept(m *types.Manifest, root, key, arg string) (manageAction, error) {
	action := manageAction{key: key, accept: true}

	recorded := ""
	if m.ManagedFiles != nil {
		recorded = m.ManagedFiles[key]
	}
	if recorded == "" {
		return action, fmt.Errorf("path %q is not recorded in [managed_files]", arg)
	}

	data, err := fileutil.SafeReadFile(root, filepath.FromSlash(key))
	if err != nil {
		if os.IsNotExist(err) {
			return action, fmt.Errorf("cannot accept %q: file does not exist", arg)
		}
		return action, fmt.Errorf("cannot accept %q: %s", arg, util.Sanitize(err.Error()))
	}
	action.hash = tasks.HashContent(data)

	if err := validateAcceptBase(root, key, recorded, &action); err != nil {
		return action, err
	}
	recordAcceptState(m, key, recorded, &action)
	return action, nil
}

// validateAcceptBase preflights the committed base against the pristine ledger
// hash. An invalid base fails so accept cannot claim success while drift stays
// red; a missing base only warns because missing bases are advisory by default.
func validateAcceptBase(root, key, recorded string, action *manageAction) error {
	switch base, baseErr := fileutil.SafeReadFile(root, tasks.BaseShadowRelPath(key)); {
	case baseErr == nil:
		if !tasks.BaseMatchesRecordedHash(base, recorded) {
			return fmt.Errorf("committed base for %q is invalid; restore it before accepting", key)
		}
	case errors.Is(baseErr, os.ErrNotExist):
		action.warnMissingBase = true
	default:
		return fmt.Errorf("cannot read committed base for %q: %s",
			key, util.Sanitize(baseErr.Error()))
	}
	return nil
}

// recordAcceptState records the accepted hash on m. Bytes that already match
// the pristine render have nothing to accept, so a stale accepted baseline is
// cleared instead; bytes that already match an accepted baseline are a no-op.
func recordAcceptState(m *types.Manifest, key, recorded string, action *manageAction) {
	if action.hash == recorded {
		if _, ok := m.AcceptedFiles[key]; ok {
			action.cleared = true
			delete(m.AcceptedFiles, key)
			return
		}
		action.alreadyCurrent = true
		return
	}

	if m.AcceptedFiles == nil {
		m.AcceptedFiles = make(map[string]string)
	}
	if m.AcceptedFiles[key] == action.hash {
		action.alreadyCurrent = true
		return
	}
	m.AcceptedFiles[key] = action.hash
}

// planUnmanage validates one unmanage argument and records the detach on m. The
// working file is not read, so a missing or symlinked leaf is allowed as long as
// the key is lexically contained and any committed base is a regular file.
func planUnmanage(m *types.Manifest, root, key, arg string) (manageAction, error) {
	action := manageAction{key: key}
	action.baseRel = filepath.ToSlash(tasks.BaseShadowRelPath(key))

	_, managed := m.ManagedFiles[key]
	already := listsUnmanagedKey(m, key)
	if !managed && !already {
		return action, fmt.Errorf("path %q is not recorded in [managed_files]", arg)
	}
	if key == fileutil.GitIgnore {
		return action, errors.New("root .gitignore cannot be unmanaged; use accept or edit the scbake block manually")
	}

	baseAbs, removeBase, err := inspectUnmanageBase(root, key, action.baseRel)
	if err != nil {
		return action, err
	}
	action.baseAbs = baseAbs
	action.removeBase = removeBase
	action.alreadyUnmanaged = already && !managed && !action.removeBase

	if managed || !already {
		delete(m.ManagedFiles, key)
		delete(m.AcceptedFiles, key)
		m.UnmanagedFiles = tasks.NormalizeUnmanagedFiles(append(m.UnmanagedFiles, key))
	}
	return action, nil
}

// inspectUnmanageBase resolves and inspects the committed base for key. It
// returns the absolute base path and whether it is safe to remove. A non-regular
// base fails closed because removing it could delete a directory or follow a
// symlink outside the root.
func inspectUnmanageBase(root, key, baseRel string) (string, bool, error) {
	baseAbs, err := fileutil.SafePath(root, filepath.FromSlash(baseRel))
	if err != nil {
		return "", false, fmt.Errorf("invalid base shadow path for %q: %s",
			key, util.Sanitize(err.Error()))
	}
	info, statErr := os.Lstat(baseAbs)
	if statErr == nil {
		if !info.Mode().IsRegular() {
			return "", false, fmt.Errorf("committed base for %q is not a regular file; refusing to remove it", key)
		}
		return baseAbs, true, nil
	}
	if !os.IsNotExist(statErr) {
		return "", false, fmt.Errorf("failed to inspect committed base for %q: %s",
			key, util.Sanitize(statErr.Error()))
	}
	return baseAbs, false, nil
}

// changed reports whether the action mutates persistent state.
func (a manageAction) changed() bool {
	if a.accept {
		return !a.alreadyCurrent
	}
	return !a.alreadyUnmanaged
}

// removeUnmanagedBases tracks and removes each scheduled base shadow once.
// Tracking before removal lets rollback restore the file.
func removeUnmanagedBases(tx *transaction.Manager, actions []manageAction) error {
	removed := make(map[string]struct{})
	for _, action := range actions {
		if !action.removeBase {
			continue
		}
		if _, ok := removed[action.baseAbs]; ok {
			continue
		}
		removed[action.baseAbs] = struct{}{}
		if err := tx.Track(action.baseAbs); err != nil {
			return fmt.Errorf("failed to track base shadow %s: %s",
				util.Sanitize(action.baseAbs), util.Sanitize(err.Error()))
		}
		if err := os.Remove(action.baseAbs); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove base shadow %s: %s",
				util.Sanitize(action.baseAbs), util.Sanitize(err.Error()))
		}
	}
	return nil
}

// reportManageActions prints one stable line per argument. Dry runs phrase the
// action as a plan, never as a completed mutation.
func reportManageActions(actions []manageAction, accept, dryRun bool) {
	for _, action := range actions {
		if accept {
			fmt.Println(describeAcceptAction(action, dryRun))
			continue
		}
		fmt.Println(describeUnmanageAction(action, dryRun))
	}
}

// describeAcceptAction phrases one accept result. Dry runs phrase the action as
// a plan, never as a completed mutation.
func describeAcceptAction(action manageAction, dryRun bool) string {
	key := util.Sanitize(action.key)
	switch {
	case action.alreadyCurrent:
		return fmt.Sprintf("ℹ️  %s already matches its current baseline", key)
	case dryRun && action.cleared:
		return fmt.Sprintf("Would clear accepted baseline for %s (file matches its recorded hash)", key)
	case dryRun:
		return fmt.Sprintf("Would accept %s (sha256 %s)", key, shortContentHash(action.hash))
	case action.cleared:
		return fmt.Sprintf("✅ Cleared accepted baseline for %s (file matches its recorded hash)", key)
	default:
		return fmt.Sprintf("✅ Accepted %s (sha256 %s)", key, shortContentHash(action.hash))
	}
}

// describeUnmanageAction phrases one unmanage result, using plan wording for a
// dry run.
func describeUnmanageAction(action manageAction, dryRun bool) string {
	key := util.Sanitize(action.key)
	baseRel := util.Sanitize(action.baseRel)
	switch {
	case action.alreadyUnmanaged:
		return fmt.Sprintf("ℹ️  %s is already unmanaged", key)
	case dryRun && action.removeBase:
		return fmt.Sprintf("Would unmanage %s (remove committed base %s; working file kept)",
			key, baseRel)
	case dryRun:
		return fmt.Sprintf("Would unmanage %s (working file kept)", key)
	case action.removeBase:
		return fmt.Sprintf("✅ Unmanaged %s (removed committed base %s; working file kept)",
			key, baseRel)
	default:
		return fmt.Sprintf("✅ Unmanaged %s (working file kept)", key)
	}
}

// reportManageWarnings prints deferred advisory warnings after validation.
func reportManageWarnings(actions []manageAction) {
	for _, action := range actions {
		if action.warnMissingBase {
			fmt.Fprintf(os.Stderr,
				"⚠️  No committed base for %s; accepting without base validation (use 'scbake drift --require-base' to make a missing base an error).\n",
				util.Sanitize(action.key))
		}
	}
}

// shortContentHash abbreviates a SHA-256 for human-readable output. The value
// is sanitized before display because it is retained from untrusted manifest
// content.
func shortContentHash(hash string) string {
	const n = 12
	if len(hash) > n {
		hash = hash[:n]
	}
	return util.Sanitize(hash)
}
