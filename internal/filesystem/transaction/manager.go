// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
	"sync"
	"time"
)

// ErrCommitted marks a Commit error that happened after the committed journal
// record became durable. The transaction's changes are permanent even though
// cleanup reported an error; callers must not roll back or delete the result.
var ErrCommitted = errors.New("transaction committed with cleanup error")

// staleTransactionThreshold is the minimum age a transaction backup directory
// must reach before a startup sweep reclaims it. A live apply touches its
// transaction directory on every backup and the apply timeout is 30 minutes,
// so a directory idle for a full hour can only be the residue of a crash
// (panic/SIGKILL/power loss) that skipped both Commit and Rollback.
const staleTransactionThreshold = time.Hour

// txDirPrefix identifies transaction backup directories created by ensureTempDir.
const txDirPrefix = "tx-"

// backupEntry holds the metadata required to restore a file.
type backupEntry struct {
	tempPath string
	mode     os.FileMode
}

// Manager handles atomic filesystem operations by tracking changes and providing rollback capabilities.
type Manager struct {
	mu sync.Mutex

	// rootPath is the absolute path to the project root.
	// Safety check: The manager will refuse to touch files outside this root.
	rootPath string

	// tempDir is the hidden directory where backups for this specific transaction are stored.
	// It is lazily created only when the first backup is needed.
	tempDir string

	// backups maps the original absolute path to its backup metadata.
	backups map[string]backupEntry

	// created tracks absolute paths of files/dirs created during the transaction.
	// They are stored in order of creation (append-only) for LIFO deletion.
	created []string

	// createdSet mirrors created for O(1) membership tests in alreadyTracked.
	// The two hold exactly the same paths: every element of created is in the
	// set and vice versa. It exists only to avoid a linear scan that made
	// tracking Θ(n²) in the number of created paths.
	createdSet map[string]struct{}

	// journalPath is the path of the transaction journal, once one has been
	// created. Journal appends open, fsync, and close the file so no handle is
	// left open on Windows.
	journalPath string

	// removeRootOnRecovery marks a transaction whose root was created by
	// `scbake new`; recovering it removes the whole bootstrap directory.
	removeRootOnRecovery bool

	// committed records that the transaction was finalized with Commit().
	// After a commit, Rollback() must be a no-op: the deferred rollback safety
	// net (RunApply) runs after Commit() even on success.
	committed bool
}

// Options controls transaction setup.
type Options struct {
	// RemoveRootOnRecovery records that the transaction root was created by
	// `scbake new`, so recovering an interrupted run removes the bootstrap
	// directory along with the partial project.
	RemoveRootOnRecovery bool
}

// New creates a new transaction manager scoped to the given project root.
func New(rootPath string) (*Manager, error) {
	return NewWithOptions(rootPath, Options{})
}

// NewWithOptions creates a transaction manager with explicit options.
func NewWithOptions(rootPath string, opts Options) (*Manager, error) {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute root path: %w", err)
	}

	// Reclaim committed or legacy residue, then refuse to start a new
	// transaction while an interrupted one still needs recovery. A failure here
	// must prevent new mutations: recovering the previous run is a precondition.
	sweepStaleTransactions(absRoot)
	pending, err := ListPending(absRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect transaction state: %w", err)
	}
	for _, tx := range pending {
		if tx.Alive {
			continue
		}
		return nil, &IncompleteTransactionError{Dir: tx.Dir, PID: tx.PID}
	}

	manager := &Manager{
		rootPath:             absRoot,
		backups:              make(map[string]backupEntry),
		created:              make([]string, 0),
		createdSet:           make(map[string]struct{}),
		removeRootOnRecovery: opts.RemoveRootOnRecovery,
	}
	if opts.RemoveRootOnRecovery {
		// `new` must be recoverable even if it is killed before the first
		// tracked mutation (for example during planning or preflight).
		if err := manager.startJournal(); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

// resolvedTmpDir returns the symlink-resolved <root>/.scbake/tmp directory, but
// only when it lies inside the resolved root. ok is false when the directory is
// missing or resolves outside the root (for example a symlinked .scbake/tmp);
// the manager must never delete paths outside its root.
func resolvedTmpDir(rootPath string) (string, bool) {
	resolvedRoot, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return "", false
	}
	resolvedTmp, err := filepath.EvalSymlinks(filepath.Join(rootPath, fileutil.InternalDir, fileutil.TmpDir))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedTmp)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return resolvedTmp, true
}

// Root returns the absolute project root the transaction is scoped to. Callers
// that create paths inside the root use it to bound ancestor tracking.
func (m *Manager) Root() string {
	return m.rootPath
}

// Commit finalizes the transaction by deleting the temporary backup directory and pruning structural scaffolding.
// This should be called only after all tasks have succeeded.
// After Commit, Rollback is a no-op: the transaction's changes are permanent.
func (m *Manager) Commit() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.committed {
		return nil
	}

	// Record the commit durably before deleting the backups. If the process
	// dies after this point, a later run knows the transaction succeeded and can
	// reclaim the directory instead of treating it as interrupted.
	if m.tempDir != "" {
		durable, err := m.appendJournalRecord(journalRecord{Op: journalOpCommitted})
		if durable {
			// The committed record is on disk. A later Close error must be
			// returned to the caller but must not re-enable deferred rollback.
			m.committed = true
		}
		if err != nil {
			return m.commitError(err)
		}
	}
	m.committed = true

	// If tempDir was never created, there is nothing to clean up.
	if m.tempDir != "" {
		// Remove the temp directory and all backed-up files.
		if err := os.RemoveAll(m.tempDir); err != nil {
			return m.commitError(fmt.Errorf("failed to cleanup transaction temp dir: %w", err))
		}

		// Prune parent scaffolding if empty (.scbake/tmp and .scbake)
		m.cleanupStructure()
	}

	// Reset internal state so a trailing Rollback cannot undo committed changes.
	m.resetState()

	return nil
}

// commitError marks errors that occur after the point of no return. Callers
// treat those as committed-with-warning rather than undoing the result.
func (m *Manager) commitError(err error) error {
	if err == nil {
		return nil
	}
	if !m.committed {
		return err
	}
	return fmt.Errorf("%w: %w", ErrCommitted, err)
}

// ensureTempDir creates the hidden temporary directory if it doesn't exist.
// This is called lazily by Track().
func (m *Manager) ensureTempDir() error {
	if m.tempDir != "" {
		return nil
	}

	tmpParent := filepath.Join(m.rootPath, fileutil.InternalDir, fileutil.TmpDir)

	// Refuse to create the transaction directory through a symlink that escapes
	// the root (e.g. a symlinked .scbake/tmp), before anything is written.
	if _, err := fileutil.SafePath(m.rootPath, tmpParent); err != nil {
		return fmt.Errorf("refusing to create transaction temp dir: %w", err)
	}

	// We place tmp inside the project root to ensure we are on the same filesystem/partition,
	// which makes file moves atomic and avoids cross-device link errors.
	if err := os.MkdirAll(tmpParent, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create temp parent dir %s: %w", tmpParent, err)
	}

	// Create the temp dir under a random name so an existing path is never
	// reused. Keep MkdirTemp's 0700 mode: backups are private to the process,
	// and chmod would only widen the window for a swapped symlink.
	path, err := os.MkdirTemp(tmpParent, txDirPrefix+"*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	m.tempDir = path
	return nil
}

// cleanupStructure attempts to prune .scbake/tmp and .scbake.
// It uses os.Remove which only succeeds if the directory is empty.
func (m *Manager) cleanupStructure() {
	tmpParent := filepath.Join(m.rootPath, fileutil.InternalDir, fileutil.TmpDir)
	scbakeRoot := filepath.Join(m.rootPath, fileutil.InternalDir)

	// Best effort removal of parent directories
	_ = os.Remove(tmpParent)
	_ = os.Remove(scbakeRoot)
}

// resetState clears the internal trackers for a fresh state.
func (m *Manager) resetState() {
	m.tempDir = ""
	m.journalPath = ""
	m.backups = make(map[string]backupEntry)
	m.created = make([]string, 0)
	m.createdSet = make(map[string]struct{})
}
