// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

	// journalNonceBytes is the size of the random identifier in the journal
	// header. The matching run marker lives outside the project so a journal
	// copied through Git does not authorize recovery in the checkout.
	journalNonceBytes = 16

	runMarkerPrefix = "scbake-tx-"
	runMarkerSuffix = ".marker"

	// runMarkerDirPrefix names the private per-user directory under the system
	// temporary directory. Markers never live directly in the shared temp
	// root, where another local user could pre-create or spoof them.
	runMarkerDirPrefix = "scbake-markers-"
	runMarkerDirPerms  = 0o700

	maxRunMarkerBytes = 4096

	// unverifiedLivenessWindow bounds how long a PID-only check may keep a
	// transaction alive when the platform cannot provide a process start token.
	// The apply timeout is 30 minutes, so an hour-old transaction idled by a
	// recycled PID is treated as dead.
	unverifiedLivenessWindow = staleTransactionThreshold
)

// journalRecord is one JSON line in a transaction journal. The first line
// carries the header fields (Version, PID, Started); later lines carry an Op.
type journalRecord struct {
	Version    int       `json:"version,omitempty"`
	PID        int       `json:"pid,omitempty"`
	OwnerStart string    `json:"owner_start,omitempty"`
	Nonce      string    `json:"nonce,omitempty"`
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
	Nonce      string
	Started    time.Time
	Entries    []journalRecord
	Committed  bool
	RemoveRoot bool
}

// PendingTransaction is a transaction directory with an uncommitted journal.
// Alive reports whether the process that created it is still running. Trusted
// reports whether the journal has a matching local run marker; a journal copied
// through Git has none and must not be replayed without explicit opt-in.
type PendingTransaction struct {
	Dir        string
	PID        int
	OwnerStart string
	Alive      bool
	Trusted    bool
	// LivenessVerifiable reports whether Alive came from a process start-token
	// comparison rather than a PID-only fallback.
	LivenessVerifiable bool
}

// RecoverOptions controls how Recover handles pending transactions.
type RecoverOptions struct {
	// Discard removes transaction directories without restoring their backups.
	Discard bool
	// Force allows replaying a journal that has no matching local run marker,
	// for example after the marker was removed with the system temp directory.
	Force bool
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

	nonce, err := newJournalNonce()
	if err != nil {
		return fmt.Errorf("failed to generate transaction identifier: %w", err)
	}
	// The marker is deliberately outside the project: it cannot be committed,
	// so a journal found in a checkout does not authorize automatic recovery.
	// Marker creation is best-effort; a failure only means recovery needs the
	// explicit --force opt-in later.
	m.markerPath, _ = writeRunMarker(nonce, m.rootPath)

	journalPath := filepath.Join(m.tempDir, journalFileName)
	f, err := fileutil.SafeOpenFile(
		m.tempDir,
		journalFileName,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		fileutil.PrivateFilePerms,
	)
	if err != nil {
		m.removeRunMarker()
		return fmt.Errorf("failed to create transaction journal: %w", err)
	}

	header := journalRecord{
		Version:    journalVersion,
		PID:        os.Getpid(),
		OwnerStart: processStartToken(os.Getpid()),
		Nonce:      nonce,
		Started:    time.Now().UTC(),
		RemoveRoot: m.removeRootOnRecovery,
	}
	if err := writeJournalRecord(f, header); err != nil {
		_ = f.Close()
		_ = os.Remove(journalPath)
		m.removeRunMarker()
		return fmt.Errorf("failed to write transaction journal header: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(journalPath)
		m.removeRunMarker()
		return fmt.Errorf("failed to sync transaction journal: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(journalPath)
		m.removeRunMarker()
		return fmt.Errorf("failed to close transaction journal: %w", err)
	}

	m.journalPath = journalPath
	return nil
}

// appendJournal writes and fsyncs one durable record before the caller mutates
// the path it describes. The journal is closed again before returning.
func (m *Manager) appendJournal(record journalRecord) error {
	if err := m.startJournal(); err != nil {
		return err
	}

	f, err := fileutil.SafeOpenFile(
		m.tempDir,
		journalFileName,
		os.O_WRONLY|os.O_APPEND,
		fileutil.PrivateFilePerms,
	)
	if err != nil {
		return fmt.Errorf("failed to open transaction journal: %w", err)
	}
	if err := writeJournalRecord(f, record); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to append transaction journal: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to sync transaction journal: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close transaction journal: %w", err)
	}
	return nil
}

// writeJournalRecord writes one JSON object followed by a newline.
func writeJournalRecord(f *os.File, record journalRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// newJournalNonce creates the random identifier shared by a journal and its
// local run marker.
func newJournalNonce() (string, error) {
	buf := make([]byte, journalNonceBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// validJournalNonce rejects a malformed nonce before it can be used as a file
// name. The journal is untrusted input after it has been copied around.
func validJournalNonce(nonce string) bool {
	if len(nonce) != journalNonceBytes*2 {
		return false
	}
	_, err := hex.DecodeString(nonce)
	return err == nil
}

// runMarkerPath returns the local marker path for a journal nonce. The marker
// lives in a private per-user directory, never under the project root, so it
// cannot be committed and then replayed from a clone.
func runMarkerPath(nonce, rootPath string) (string, error) {
	dir, err := runMarkerDir(rootPath, false)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, runMarkerPrefix+nonce+runMarkerSuffix), nil
}

// runMarkerDir returns the private per-user directory that holds run markers.
// It must stay outside the canonical project root: a relative TMPDIR such as
// "tmp" would otherwise place the marker inside a project that has a tmp/
// directory, where `git add .` would commit it. When create is true the
// directory is created with mode 0700; otherwise it must already exist.
func runMarkerDir(rootPath string, create bool) (string, error) {
	base, err := absoluteTempDir()
	if err != nil {
		return "", err
	}
	project := canonicalRoot(rootPath)
	if project == "" {
		return "", fmt.Errorf("failed to resolve project root %s", rootPath)
	}
	if pathWithin(project, base) {
		return "", fmt.Errorf(
			"temporary directory %s is inside project root %s; set TMPDIR to a directory outside the project",
			base, project,
		)
	}

	dir := filepath.Join(base, runMarkerDirPrefix+currentUserToken())
	if create {
		if err := os.MkdirAll(dir, runMarkerDirPerms); err != nil {
			return "", fmt.Errorf("failed to create run marker directory: %w", err)
		}
		// MkdirAll keeps the mode of an existing directory, so set it
		// explicitly. Ownership is still verified below before it is trusted.
		_ = os.Chmod(dir, runMarkerDirPerms)
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !markerDirTrusted(info) {
		return "", fmt.Errorf("refusing untrusted run marker directory %s", dir)
	}
	return dir, nil
}

// absoluteTempDir resolves os.TempDir to an absolute, symlink-free path. A
// relative TMPDIR is resolved against the current directory; callers reject it
// when that lands inside the project.
func absoluteTempDir() (string, error) {
	tmp := os.TempDir()
	if tmp == "" {
		return "", errors.New("temporary directory is not configured")
	}
	abs, err := filepath.Abs(tmp)
	if err != nil {
		return "", fmt.Errorf("failed to resolve temporary directory %q: %w", tmp, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve temporary directory %q: %w", abs, err)
	}
	return resolved, nil
}

// pathWithin reports whether target is base or lies below it.
func pathWithin(base, target string) bool {
	if base == "" || target == "" {
		return false
	}
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	if runtime.GOOS == "windows" {
		base = strings.ToLower(base)
		target = strings.ToLower(target)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// canonicalRoot resolves the project root so a marker stays valid when the
// project is addressed through a symlinked spelling.
func canonicalRoot(rootPath string) string {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return filepath.Clean(abs)
	}
	return resolved
}

// sameRoot compares two canonical roots, accounting for Windows case folding.
func sameRoot(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// writeRunMarker records that this project's transaction was started on this
// machine. The marker lives in the system temp directory, not in the project,
// so it cannot be committed and then replayed from a clone.
func writeRunMarker(nonce, rootPath string) (string, error) {
	canonical := canonicalRoot(rootPath)
	if canonical == "" {
		return "", fmt.Errorf("failed to resolve project root %s", rootPath)
	}
	dir, err := runMarkerDir(rootPath, true)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, runMarkerPrefix+nonce+runMarkerSuffix)
	f, err := fileutil.SafeOpenFile(
		dir,
		filepath.Base(path),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		fileutil.PrivateFilePerms,
	)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(canonical + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// journalTrusted reports whether a journal has a matching marker created by a
// local run of this project. Missing, malformed, or mismatched markers mean the
// journal may have arrived through Git or a copy and must not be replayed
// automatically.
func journalTrusted(journal *transactionJournal, rootPath string) bool {
	if !validJournalNonce(journal.Nonce) {
		return false
	}
	path, err := runMarkerPath(journal.Nonce, rootPath)
	if err != nil {
		return false
	}
	// Lstat before opening: a FIFO at the marker path would otherwise block a
	// read-only command such as `recover --dry-run` forever.
	info, err := os.Lstat(path)
	if err != nil || !markerFileTrusted(info) {
		return false
	}
	f, err := fileutil.SafeOpenFile(filepath.Dir(path), filepath.Base(path), os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	// Re-check the opened handle so a swap between Lstat and open cannot turn
	// a regular marker into another file type.
	opened, err := f.Stat()
	if err != nil || !markerFileTrusted(opened) {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRunMarkerBytes+1))
	if err != nil || len(data) > maxRunMarkerBytes {
		return false
	}
	return sameRoot(strings.TrimSpace(string(data)), canonicalRoot(rootPath))
}

// journalOwnerAlive reports whether the recorded owner is still running. When
// the platform cannot compare a process start token, a PID match is only
// trusted for a bounded age so PID reuse cannot block recovery forever.
func journalOwnerAlive(journal *transactionJournal, now time.Time) bool {
	if journal.PID <= 0 {
		return false
	}
	if !processStartTokensAvailable || journal.OwnerStart == "" {
		if journal.Started.IsZero() || now.Sub(journal.Started) > unverifiedLivenessWindow {
			return false
		}
	}
	return processAlive(journal.PID, journal.OwnerStart)
}

// journalOwnerVerifiablyAlive reports a live owner only when the platform can
// prove the PID is the same process incarnation. Callers use it to decide
// whether an explicit --discard should still be refused.
func journalOwnerVerifiablyAlive(journal *transactionJournal) bool {
	if !processStartTokensAvailable || journal.OwnerStart == "" {
		return false
	}
	return processAlive(journal.PID, journal.OwnerStart)
}

// removeJournalMarker deletes the marker for a transaction recovered from its
// journal.
func removeJournalMarker(journal *transactionJournal, rootPath string) {
	if !validJournalNonce(journal.Nonce) {
		return
	}
	path, err := runMarkerPath(journal.Nonce, rootPath)
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

// removeRunMarker deletes the local marker for a completed transaction.
func (m *Manager) removeRunMarker() {
	if m.markerPath == "" {
		return
	}
	_ = os.Remove(m.markerPath)
	m.markerPath = ""
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
		sweepTransactionEntry(rootPath, tmpDir, entry, cutoff)
	}
}

// sweepTransactionEntry reclaims one transaction directory when it is provably
// unrecoverable: committed with a dead owner, or journal-less and stale.
func sweepTransactionEntry(rootPath, tmpDir string, entry os.DirEntry, cutoff time.Time) {
	// entry.IsDir() is false for symlink entries, so a tx-* symlink is
	// skipped rather than followed.
	if !entry.IsDir() || !strings.HasPrefix(entry.Name(), txDirPrefix) {
		return
	}

	dir := filepath.Join(tmpDir, entry.Name())
	journal, err := readTransactionJournal(dir)
	switch {
	case err == nil:
		if journal.Committed && !journalOwnerAlive(journal, time.Now().UTC()) {
			_ = os.RemoveAll(dir)
			removeJournalMarker(journal, rootPath)
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
	j.Nonce = record.Nonce
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
func listTransactionDirs(rootPath string) ([]string, error) {
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
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), txDirPrefix) {
			continue
		}
		dirs = append(dirs, filepath.Join(tmpDir, entry.Name()))
	}
	return dirs, nil
}

// ListPending returns every uncommitted transaction journal under root. A
// malformed journal is an error rather than a silently ignored transaction.
func ListPending(rootPath string) ([]PendingTransaction, error) {
	dirs, err := listTransactionDirs(rootPath)
	if err != nil {
		return nil, err
	}

	pending := make([]PendingTransaction, 0, len(dirs))
	for _, dir := range dirs {
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
			Dir:                dir,
			PID:                journal.PID,
			OwnerStart:         journal.OwnerStart,
			Alive:              journalOwnerAlive(journal, time.Now().UTC()),
			Trusted:            journalTrusted(journal, rootPath),
			LivenessVerifiable: processStartTokensAvailable && journal.OwnerStart != "",
		})
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
	return RecoverWithOptions(rootPath, RecoverOptions{Discard: discard})
}

// RecoverWithOptions rolls back pending transactions with explicit controls.
// A journal without a matching local run marker is refused unless the caller
// explicitly opts in with Force, so a journal committed to a repository cannot
// rewrite or delete files in a fresh checkout.
func RecoverWithOptions(rootPath string, opts RecoverOptions) (int, error) {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve root path: %w", err)
	}

	if opts.Discard {
		return discardTransactions(absRoot)
	}

	pending, err := ListPending(absRoot)
	if err != nil {
		return 0, err
	}

	// Check liveness and provenance before mutating anything so a live run or a
	// journal copied into the tree cannot be touched by accident.
	for _, p := range pending {
		if p.Alive {
			return 0, fmt.Errorf("transaction in %s is still running (pid %d)", p.Dir, p.PID)
		}
		if !p.Trusted && !opts.Force {
			return 0, fmt.Errorf(
				"transaction in %s has no local run marker; it may belong to another checkout: rerun with --force to replay it or --discard to remove it",
				p.Dir,
			)
		}
	}

	recovered := 0
	for _, p := range pending {
		if err := recoverTransaction(absRoot, p.Dir); err != nil {
			return recovered, err
		}
		recovered++
	}

	return recovered, nil
}

// discardTransactions removes every transaction directory without parsing its
// journal. An explicit --discard must work for empty or malformed journals,
// otherwise the transaction cannot be cleared. A readable journal that names a
// live owner is still refused.
func discardTransactions(rootPath string) (int, error) {
	dirs, err := listTransactionDirs(rootPath)
	if err != nil {
		return 0, err
	}

	for _, dir := range dirs {
		// Only a verifiable incarnation blocks an explicit discard. When the
		// platform cannot prove the PID belongs to the recorded owner, --discard
		// is the escape hatch for a stuck PID-reuse false positive.
		if journal, err := readTransactionJournal(dir); err == nil &&
			!journal.Committed && journalOwnerVerifiablyAlive(journal) {
			return 0, fmt.Errorf("transaction in %s is still running (pid %d)", dir, journal.PID)
		}
	}

	for i, dir := range dirs {
		journal, journalErr := readTransactionJournal(dir)
		if err := os.RemoveAll(dir); err != nil {
			return i, fmt.Errorf("failed to discard transaction %s: %w", dir, err)
		}
		if journalErr == nil {
			removeJournalMarker(journal, rootPath)
		}
	}

	return len(dirs), nil
}

// recoverTransaction replays the journal in reverse, then removes the
// transaction directory.
func recoverTransaction(rootPath, dir string) error {
	journal, err := readTransactionJournal(dir)
	if err != nil {
		return err
	}
	if journal.Committed {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to clean committed transaction %s: %w", dir, err)
		}
		removeJournalMarker(journal, rootPath)
		return nil
	}

	manager, err := managerFromJournal(rootPath, dir, journal)
	if err != nil {
		return err
	}
	if err := manager.Rollback(); err != nil {
		return fmt.Errorf("failed to recover transaction %s: %w", dir, err)
	}

	// `new` creates the project directory and bootstrap manifest before the
	// transaction starts. Its journal marks that root, so a successful recovery
	// removes the whole half-created project, matching the command's own
	// handled-error cleanup.
	if journal.RemoveRoot {
		if err := os.RemoveAll(rootPath); err != nil {
			return fmt.Errorf("failed to remove bootstrapped project root %s: %w", rootPath, err)
		}
	}
	removeJournalMarker(journal, rootPath)
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
