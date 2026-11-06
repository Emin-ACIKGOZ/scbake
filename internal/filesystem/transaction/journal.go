// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
	"time"
)

const (
	// journalFileName is the on-disk record of every path a transaction has
	// touched. It is written and fsynced before the corresponding mutation so a
	// process killed after the write can be recovered on the next run.
	journalFileName = "journal.jsonl"

	journalVersion = 1

	journalOpCreate    = "create"
	journalOpBackup    = "backup"
	journalOpCommitted = "committed"
)

// journalRecord is one JSON line in a transaction journal. The first line
// carries the header fields (Version, PID, Started); later lines carry an Op.
type journalRecord struct {
	Version    int       `json:"version,omitempty"`
	PID        int       `json:"pid,omitempty"`
	OwnerStart string    `json:"owner_start,omitempty"`
	Started    time.Time `json:"started,omitempty"`

	Op     string `json:"op,omitempty"`
	Path   string `json:"path,omitempty"`
	Backup string `json:"backup,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`

	// RemoveRoot marks a transaction whose root directory was created by
	// `scbake new`. Recovering it removes the whole bootstrap directory, the
	// same way the command's handled-error cleanup does.
	RemoveRoot bool `json:"remove_root,omitempty"`
}

// transactionJournal is the parsed form of a journal file.
type transactionJournal struct {
	PID        int
	OwnerStart string
	Started    time.Time
	Entries    []journalRecord
	Committed  bool
	RemoveRoot bool
}

// PendingTransaction is a transaction directory with an uncommitted journal.
// Alive reports whether the process that created it is still running.
type PendingTransaction struct {
	Dir        string
	PID        int
	OwnerStart string
	Alive      bool
}

// ErrIncompleteTransaction identifies a transaction that was interrupted after
// it began mutating the filesystem and has not been rolled back or committed.
var ErrIncompleteTransaction = errors.New("incomplete transaction")

// IncompleteTransactionError describes a transaction left by a dead process.
type IncompleteTransactionError struct {
	Dir string
	PID int
}

func (e *IncompleteTransactionError) Error() string {
	return fmt.Sprintf(
		"incomplete transaction in %s (pid %d); run 'scbake recover' to roll it back or 'scbake recover --discard' to discard it",
		e.Dir, e.PID,
	)
}

// Unwrap allows callers to test for ErrIncompleteTransaction.
func (e *IncompleteTransactionError) Unwrap() error {
	return ErrIncompleteTransaction
}

// startJournal creates the journal and writes its header. The file is closed
// immediately; each append reopens it so Windows never sees a lingering handle.
func (m *Manager) startJournal() error {
	if m.journalPath != "" {
		return nil
	}
	if err := m.ensureTempDir(); err != nil {
		return err
	}

	journalPath := filepath.Join(m.tempDir, journalFileName)
	f, err := fileutil.SafeOpenFile(
		m.tempDir,
		journalFileName,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		fileutil.PrivateFilePerms,
	)
	if err != nil {
		return fmt.Errorf("failed to create transaction journal: %w", err)
	}

	header := journalRecord{
		Version:    journalVersion,
		PID:        os.Getpid(),
		OwnerStart: processStartToken(os.Getpid()),
		Started:    time.Now().UTC(),
		RemoveRoot: m.removeRootOnRecovery,
	}
	if err := writeJournalRecord(f, header); err != nil {
		_ = f.Close()
		_ = os.Remove(journalPath)
		return fmt.Errorf("failed to write transaction journal header: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(journalPath)
		return fmt.Errorf("failed to sync transaction journal: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(journalPath)
		return fmt.Errorf("failed to close transaction journal: %w", err)
	}

	m.journalPath = journalPath
	return nil
}

// journalFile is the subset of *os.File needed to append a durable journal
// record. It is an interface only so tests can inject a Close failure that
// happens after a successful Sync.
type journalFile interface {
	io.Writer
	Sync() error
	Close() error
}

// openJournalForAppend opens the journal for appending. It is a variable only
// so tests can wrap the file and simulate a Close error after Sync.
var openJournalForAppend = func(baseDir, targetPath string, flag int, perm os.FileMode) (journalFile, error) {
	return fileutil.SafeOpenFile(baseDir, targetPath, flag, perm)
}

// journalCloseErrorHookForTest lets tests make a journal Close fail after a
// successful Sync for a specific record operation. Production leaves it nil.
var journalCloseErrorHookForTest func(op string) error

// SetJournalCloseErrorHookForTest installs a hook that can fail a journal
// Close after Sync and returns a function that restores the previous hook. It
// exists only so end-to-end tests can inject a post-sync close failure; it is
// not intended for production callers.
func SetJournalCloseErrorHookForTest(hook func(op string) error) (restore func()) {
	previous := journalCloseErrorHookForTest
	journalCloseErrorHookForTest = hook
	return func() { journalCloseErrorHookForTest = previous }
}

// appendJournal writes and fsyncs one durable record. durable reports whether
// the record reached disk, independent of a later Close error. Callers must
// keep any state described by a durable record even when err is non-nil.
func (m *Manager) appendJournal(record journalRecord) (durable bool, err error) {
	return m.appendJournalRecord(record)
}

// appendJournalRecord writes and fsyncs one durable record before the caller
// mutates the path it describes. durable reports whether the record reached
// disk, independent of a later Close error. Callers that treat the record as a
// point of no return must honor durable even when err is non-nil.
func (m *Manager) appendJournalRecord(record journalRecord) (durable bool, err error) {
	if err := m.startJournal(); err != nil {
		return false, err
	}

	f, err := openJournalForAppend(
		m.tempDir,
		journalFileName,
		os.O_WRONLY|os.O_APPEND,
		fileutil.PrivateFilePerms,
	)
	if err != nil {
		return false, fmt.Errorf("failed to open transaction journal: %w", err)
	}
	if err := writeJournalRecord(f, record); err != nil {
		_ = f.Close()
		return false, fmt.Errorf("failed to append transaction journal: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return false, fmt.Errorf("failed to sync transaction journal: %w", err)
	}
	// Sync is the durability point. A Close error after it must not make the
	// caller treat the record as unwritten.
	if err := f.Close(); err != nil {
		return true, fmt.Errorf("failed to close transaction journal: %w", err)
	}
	if hook := journalCloseErrorHookForTest; hook != nil {
		if err := hook(record.Op); err != nil {
			return true, fmt.Errorf("failed to close transaction journal: %w", err)
		}
	}
	return true, nil
}

// writeJournalRecord writes one JSON object followed by a newline.
func writeJournalRecord(f io.Writer, record journalRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// sweepStaleTransactions removes transaction directories that cannot contain
// recoverable state: committed journals whose owner is gone, and directories
// without a journal that are older than staleTransactionThreshold. A
// directory with an uncommitted journal is never removed here.
func sweepStaleTransactions(rootPath string) {
	tmpDir, ok := resolvedTmpDir(rootPath)
	if !ok {
		return
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-staleTransactionThreshold)
	for _, entry := range entries {
		sweepTransactionEntry(tmpDir, entry, cutoff)
	}
}

// sweepTransactionEntry reclaims one transaction directory when it is provably
// unrecoverable: committed with a dead owner, or journal-less and stale.
func sweepTransactionEntry(tmpDir string, entry os.DirEntry, cutoff time.Time) {
	// entry.IsDir() is false for symlink entries, so a tx-* symlink is
	// skipped rather than followed.
	if !entry.IsDir() || !strings.HasPrefix(entry.Name(), txDirPrefix) {
		return
	}

	dir := filepath.Join(tmpDir, entry.Name())
	journal, err := readTransactionJournal(dir)
	switch {
	case err == nil:
		if journal.Committed && !processAlive(journal.PID, journal.OwnerStart) {
			_ = os.RemoveAll(dir)
		}
		return
	case !os.IsNotExist(err):
		// A malformed journal must be reported, not swept.
		return
	}

	info, err := entry.Info()
	if err != nil || info.ModTime().After(cutoff) {
		return
	}
	_ = os.RemoveAll(dir)
}

// readTransactionJournal parses one journal file. It fails closed on malformed
// lines so recovery never guesses at a path.
func readTransactionJournal(dir string) (*transactionJournal, error) {
	f, err := fileutil.SafeOpenFile(dir, journalFileName, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	journal := &transactionJournal{}
	reader := bufio.NewReader(f)

	first := true
	for {
		rawLine, readErr := reader.ReadString('\n')
		done, err := journal.readLine(rawLine, readErr, dir, &first)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
	}
	if first {
		return nil, fmt.Errorf("transaction journal %s is empty", dir)
	}

	return journal, nil
}

// readLine consumes one physical line. It reports done when the reader reached
// EOF. A torn final record is ignored only when it is unterminated and invalid
// JSON: the mutation it describes never started because the append failed.
func (j *transactionJournal) readLine(rawLine string, readErr error, dir string, first *bool) (bool, error) {
	terminated := strings.HasSuffix(rawLine, "\n")
	line := bytes.TrimSpace([]byte(rawLine))

	if len(line) > 0 {
		if *first {
			if err := j.readHeader(line, dir); err != nil {
				return false, err
			}
			*first = false
		} else if err := j.readEntry(line, dir); err != nil {
			if readErr == io.EOF && !terminated && !json.Valid(line) {
				return true, nil
			}
			return false, err
		}
	}

	if readErr == io.EOF {
		return true, nil
	}
	if readErr != nil {
		return false, fmt.Errorf("failed to read transaction journal %s: %w", dir, readErr)
	}
	return false, nil
}

// readHeader parses and validates the first journal line.
func (j *transactionJournal) readHeader(line []byte, dir string) error {
	var record journalRecord
	if err := json.Unmarshal(line, &record); err != nil {
		return fmt.Errorf("invalid transaction journal %s: %w", dir, err)
	}
	if record.Version != journalVersion {
		return fmt.Errorf("unsupported transaction journal version %d in %s", record.Version, dir)
	}
	if record.PID <= 0 {
		return fmt.Errorf("transaction journal %s has no owner pid", dir)
	}
	j.PID = record.PID
	j.OwnerStart = record.OwnerStart
	j.Started = record.Started
	j.RemoveRoot = record.RemoveRoot
	return nil
}

// readEntry parses one mutation or status line.
func (j *transactionJournal) readEntry(line []byte, dir string) error {
	var record journalRecord
	if err := json.Unmarshal(line, &record); err != nil {
		return fmt.Errorf("invalid transaction journal %s: %w", dir, err)
	}

	switch record.Op {
	case journalOpCreate, journalOpBackup:
		j.Entries = append(j.Entries, record)
	case journalOpCommitted:
		j.Committed = true
	default:
		return fmt.Errorf("unknown transaction journal operation %q in %s", record.Op, dir)
	}
	return nil
}

// listTransactionDirs returns every tx-* directory under the resolved
// .scbake/tmp. The names are only a starting point; callers must still validate
// the journal inside each directory.
func listTransactionDirs(ctx context.Context, rootPath string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tmpDir, ok := resolvedTmpDir(rootPath)
	if !ok {
		return nil, nil
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), txDirPrefix) {
			continue
		}
		dirs = append(dirs, filepath.Join(tmpDir, entry.Name()))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return dirs, nil
}

// ListPending returns every uncommitted transaction journal under root. A
// malformed journal is an error rather than a silently ignored transaction.
func ListPending(rootPath string) ([]PendingTransaction, error) {
	return ListPendingContext(context.Background(), rootPath)
}

// ListPendingContext is ListPending with cancellation support. It checks the
// context before reading the directory, between transaction directories, and
// before returning.
func ListPendingContext(ctx context.Context, rootPath string) ([]PendingTransaction, error) {
	dirs, err := listTransactionDirs(ctx, rootPath)
	if err != nil {
		return nil, err
	}

	pending := make([]PendingTransaction, 0, len(dirs))
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		journal, err := readTransactionJournal(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if journal.Committed {
			continue
		}

		pending = append(pending, PendingTransaction{
			Dir:        dir,
			PID:        journal.PID,
			OwnerStart: journal.OwnerStart,
			Alive:      processAlive(journal.PID, journal.OwnerStart),
		})
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return pending, nil
}

// CheckPending returns an error when root has an unrecovered transaction. It is
// used by read-only commands such as drift and apply --dry-run, which must not
// report a tree with a dead journal as clean.
func CheckPending(rootPath string) error {
	pending, err := ListPending(rootPath)
	if err != nil {
		return fmt.Errorf("inspecting transaction state: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	p := pending[0]
	if p.Alive {
		return fmt.Errorf("transaction in %s is still running (pid %d)", p.Dir, p.PID)
	}
	return &IncompleteTransactionError{Dir: p.Dir, PID: p.PID}
}

// Recover rolls back every pending transaction under root. When discard is
// true it removes the transaction directories without restoring their backups.
// Recovery refuses to touch a transaction whose owner process is still alive.
func Recover(rootPath string, discard bool) (int, error) {
	return RecoverContext(context.Background(), rootPath, discard)
}

// RecoverContext is Recover with cancellation support. It checks the context
// before, after, and between transaction directories, and the rollback itself
// checks between journal entries. On cancellation it returns recovered and the
// context error so the caller can map an interruption to the right exit status.
func RecoverContext(ctx context.Context, rootPath string, discard bool) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	absRoot, err := CanonicalRoot(rootPath)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve root path: %w", err)
	}

	if discard {
		return discardTransactions(ctx, absRoot)
	}

	return recoverPendingTransactions(ctx, absRoot)
}

// CanonicalRoot returns rootPath as an absolute, symlink-resolved path so
// recovery and its disclosure target the real project directory rather than an
// alias. Symlink chains and relative paths are resolved. A path that does not
// exist yet (or is a dangling alias) is returned in absolute cleaned form so
// callers can still report that no transaction exists there.
func CanonicalRoot(rootPath string) (string, error) {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return filepath.Clean(absRoot), nil
		}
		return "", err
	}
	return resolved, nil
}

// recoverPendingTransactions refuses live transactions, then recovers each dead
// one while checking the context between directories.
func recoverPendingTransactions(ctx context.Context, rootPath string) (int, error) {
	pending, err := ListPendingContext(ctx, rootPath)
	if err != nil {
		return 0, err
	}

	// Check liveness before mutating anything so a live run cannot be touched.
	for _, p := range pending {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if p.Alive {
			return 0, fmt.Errorf("transaction in %s is still running (pid %d)", p.Dir, p.PID)
		}
	}

	recovered := 0
	for _, p := range pending {
		if err := ctx.Err(); err != nil {
			return recovered, err
		}
		if err := recoverTransaction(ctx, rootPath, p.Dir); err != nil {
			return recovered, err
		}
		recovered++
	}

	if err := ctx.Err(); err != nil {
		return recovered, err
	}
	return recovered, nil
}

// discardTransactions removes every transaction directory without parsing its
// journal. An explicit --discard must work for empty or malformed journals,
// otherwise the transaction cannot be cleared. A readable journal that names a
// live owner is still refused.
func discardTransactions(ctx context.Context, rootPath string) (int, error) {
	dirs, err := listTransactionDirs(ctx, rootPath)
	if err != nil {
		return 0, err
	}
	if err := refuseLiveTransactions(ctx, dirs); err != nil {
		return 0, err
	}

	for i, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := os.RemoveAll(dir); err != nil {
			return i, fmt.Errorf("failed to discard transaction %s: %w", dir, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return len(dirs), err
	}
	return len(dirs), nil
}

// refuseLiveTransactions fails when any readable journal names a live owner.
func refuseLiveTransactions(ctx context.Context, dirs []string) error {
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if journal, err := readTransactionJournal(dir); err == nil &&
			!journal.Committed && processAlive(journal.PID, journal.OwnerStart) {
			return fmt.Errorf("transaction in %s is still running (pid %d)", dir, journal.PID)
		}
	}
	return nil
}

// recoverTransaction replays the journal in reverse, then removes the
// transaction directory.
func recoverTransaction(ctx context.Context, rootPath, dir string) error {
	journal, err := readRecoveryJournal(ctx, dir)
	if err != nil {
		return err
	}
	if journal.Committed {
		return cleanCommittedTransaction(ctx, dir)
	}

	manager, err := managerFromJournal(rootPath, dir, journal)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if journal.RemoveRoot {
		// Restore tracked state without deleting the journal yet. Root removal
		// below is the only step that consumes the retry record.
		if err := manager.rollbackChanges(ctx); err != nil {
			return fmt.Errorf("failed to recover transaction %s: %w", dir, err)
		}
		return removeBootstrappedRoot(context.WithoutCancel(ctx), rootPath)
	}
	if err := manager.RollbackContext(ctx); err != nil {
		return fmt.Errorf("failed to recover transaction %s: %w", dir, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return nil
}

// readRecoveryJournal reads one journal and checks cancellation on both sides
// of the potentially large parse.
func readRecoveryJournal(ctx context.Context, dir string) (*transactionJournal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	journal, err := readTransactionJournal(dir)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return journal, nil
}

// cleanCommittedTransaction removes a transaction whose committed record is
// already durable.
func cleanCommittedTransaction(ctx context.Context, dir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to clean committed transaction %s: %w", dir, err)
	}
	return ctx.Err()
}

// renameRootForRemoval is the rename used to stage a bootstrap root for
// deletion. Tests replace it to inject a deterministic removal failure.
var renameRootForRemoval = os.Rename

// removeBootstrappedRoot removes the project directory for an interrupted
// `new`. It first renames the root into a sibling staging directory. The rename
// is atomic: if it fails, the original root and its transaction journal are
// untouched, so recovery can be retried. Once the rename succeeds the original
// root path is gone and the staged tree is deleted.
func removeBootstrappedRoot(ctx context.Context, rootPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := filepath.Dir(rootPath)
	staging, err := os.MkdirTemp(parent, ".scbake-remove-*")
	if err != nil {
		return fmt.Errorf("failed to stage bootstrapped project root %s for removal: %w", rootPath, err)
	}
	destination := filepath.Join(staging, filepath.Base(rootPath))
	if err := renameRootForRemoval(rootPath, destination); err != nil {
		_ = os.Remove(staging)
		return fmt.Errorf("failed to remove bootstrapped project root %s: %w", rootPath, err)
	}
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("removed bootstrapped project root %s but failed to clean staging directory %s: %w", rootPath, staging, err)
	}
	return nil
}

// managerFromJournal reconstructs rollback state without trusting any path
// written by the journal: every path is revalidated against the root and the
// backup must be a regular file inside the transaction directory.
func managerFromJournal(rootPath, dir string, journal *transactionJournal) (*Manager, error) {
	manager := &Manager{
		rootPath:   rootPath,
		tempDir:    dir,
		backups:    make(map[string]backupEntry),
		created:    make([]string, 0, len(journal.Entries)),
		createdSet: make(map[string]struct{}),
	}

	for _, record := range journal.Entries {
		path, err := fileutil.SafePath(rootPath, filepath.FromSlash(record.Path))
		if err != nil {
			return nil, fmt.Errorf("invalid path in transaction journal %s: %w", dir, err)
		}
		if err := requireStrictDescendant(rootPath, path); err != nil {
			return nil, fmt.Errorf("invalid path in transaction journal %s: %w", dir, err)
		}

		switch record.Op {
		case journalOpCreate:
			if _, ok := manager.createdSet[path]; ok {
				continue
			}
			manager.created = append(manager.created, path)
			manager.createdSet[path] = struct{}{}

		case journalOpBackup:
			backupPath, err := backupPathFromJournal(dir, record.Backup)
			if err != nil {
				return nil, err
			}
			manager.backups[path] = backupEntry{
				tempPath: backupPath,
				mode:     os.FileMode(record.Mode),
			}

		default:
			return nil, fmt.Errorf("unexpected journal operation %q", record.Op)
		}
	}

	return manager, nil
}

// requireStrictDescendant rejects a journal path that resolves to the root
// itself. Recovery must never RemoveAll the whole project.
func requireStrictDescendant(rootPath, path string) error {
	rel, err := filepath.Rel(rootPath, path)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q must be strictly inside the project root", path)
	}
	return nil
}

// backupPathFromJournal validates that the recorded backup is a single regular
// file inside dir.
func backupPathFromJournal(dir, backup string) (string, error) {
	if backup == "" || backup == "." || backup == ".." || filepath.Base(backup) != backup {
		return "", fmt.Errorf("invalid backup name %q in transaction journal %s", backup, dir)
	}
	backupPath, err := fileutil.SafePath(dir, backup)
	if err != nil {
		return "", fmt.Errorf("invalid backup path in transaction journal %s: %w", dir, err)
	}
	info, err := os.Lstat(backupPath)
	if err != nil {
		return "", fmt.Errorf("missing backup %s in transaction journal %s: %w", backup, dir, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("backup %s in transaction journal %s is not a regular file", backup, dir)
	}
	return backupPath, nil
}
