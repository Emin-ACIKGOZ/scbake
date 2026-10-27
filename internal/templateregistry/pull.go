package templateregistry

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"sort"
	"strings"
	"time"
)

const (
	httpTimeout     = 60 * time.Second
	maxArchiveBytes = 10 << 20

	cacheDirPerm  os.FileMode = 0750
	cacheFilePerm os.FileMode = 0640

	// stagingDirPrefix names the per-pull staging directories.
	stagingDirPrefix = ".scbake-stage-"
)

// Pull downloads a template from a registry and caches it locally.
// Uses the registry's stored token (overridable via SCBAKE_REGISTRY_TOKEN_<NAME> env var).
func (m *Manager) Pull(registryName, templateName string) error {
	if err := ValidateName(registryName); err != nil {
		return fmt.Errorf("invalid registry name: %w", err)
	}
	if err := ValidateName(templateName); err != nil {
		return fmt.Errorf("invalid template name: %w", err)
	}

	registry := m.Get(registryName)
	if registry == nil {
		return fmt.Errorf("registry %q not found", registryName)
	}
	if err := validateSubdirectory(registry.Subdirectory); err != nil {
		return err
	}

	token := registry.Token
	envKey := "SCBAKE_REGISTRY_TOKEN_" + strings.ToUpper(strings.ReplaceAll(registryName, "-", "_"))
	if envToken := os.Getenv(envKey); envToken != "" {
		token = envToken
	}

	downloadURL := registry.URL
	templatePart := templateName
	if registry.Version != "" {
		templatePart = templateName + "/" + registry.Version
	}
	downloadURL = fmt.Sprintf("%s/%s.tar.gz", strings.TrimRight(downloadURL, "/"), templatePart)

	destDir := TemplateCachePath(m.cacheDir, registryName, templateName)
	pullCtx := pullRequest{
		downloadURL:  downloadURL,
		token:        token,
		destDir:      destDir,
		registryName: registryName,
		templateName: templateName,
		subdirectory: registry.Subdirectory,
		version:      registry.Version,
		source:       cacheSourceKey(downloadURL),
	}
	return m.pullFromURL(pullCtx)
}

// PullFromURL downloads a template archive from an arbitrary URL and caches it.
// Accepts an optional token for authentication.
func (m *Manager) PullFromURL(registryName, templateName, downloadURL, token string) error {
	if err := ValidateName(registryName); err != nil {
		return fmt.Errorf("invalid registry name: %w", err)
	}
	if err := ValidateName(templateName); err != nil {
		return fmt.Errorf("invalid template name: %w", err)
	}

	destDir := TemplateCachePath(m.cacheDir, registryName, templateName)
	pullCtx := pullRequest{
		downloadURL:  downloadURL,
		token:        token,
		destDir:      destDir,
		registryName: registryName,
		templateName: templateName,
		source:       cacheSourceKey(downloadURL),
	}
	return m.pullFromURL(pullCtx)
}

type pullRequest struct {
	downloadURL  string
	token        string
	destDir      string
	registryName string
	templateName string
	subdirectory string
	version      string
	source       string
}

func (m *Manager) pullFromURL(pr pullRequest) error {
	manifestPath := TemplateManifestPath(m.cacheDir, pr.registryName, pr.templateName)
	if err := m.validatePull(pr, manifestPath); err != nil {
		return err
	}

	// A cache hit is only trusted when the directory exists, its contents still
	// match the recorded manifest, and the manifest was fetched from the same
	// version and URL. Anything else (missing directory, missing manifest, a
	// hash mismatch, or a config change) is treated as a miss and re-downloaded.
	// Manifests written before provenance was recorded re-pull once.
	if cacheIsValid(pr, manifestPath) {
		return nil
	}

	// Do not touch the existing cache or manifest yet: a transient download or
	// extraction failure must leave a previously valid cache intact.
	// extractToCache stages the download in a temp directory and only replaces
	// destDir once extraction has fully succeeded.
	return m.downloadAndCache(pr, manifestPath)
}

// validatePull checks the request's cache configuration, names, and
// subdirectory, then re-checks that the destination and manifest paths are
// strictly inside the cache root. The containment check is enforced again
// immediately before any destructive filesystem operation (see extractToCache)
// so a bad name can never reach outside the cache.
func (m *Manager) validatePull(pr pullRequest, manifestPath string) error {
	if m.cacheDir == "" {
		return errors.New("template cache directory is not configured")
	}
	if err := ValidateName(pr.registryName); err != nil {
		return fmt.Errorf("invalid registry name: %w", err)
	}
	if err := ValidateName(pr.templateName); err != nil {
		return fmt.Errorf("invalid template name: %w", err)
	}
	if err := validateSubdirectory(pr.subdirectory); err != nil {
		return err
	}
	if err := ensureStrictlyWithin(m.cacheDir, pr.destDir); err != nil {
		return err
	}
	return ensureStrictlyWithin(m.cacheDir, manifestPath)
}

// downloadAndCache downloads the archive, stages it via extractToCache, and
// records the integrity manifest. On failure after extraction it removes the
// freshly downloaded tree and any partial manifest, re-checking containment
// immediately before each removal (see removeAllContained) in case destDir was
// swapped for an escaping symlink while the body was read.
func (m *Manager) downloadAndCache(pr pullRequest, manifestPath string) error {
	resp, err := doDownload(pr.downloadURL, pr.token, pr.templateName)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if err := extractToCache(resp.Body, m.cacheDir, pr.destDir, pr.subdirectory); err != nil {
		return err
	}

	hash, err := hashTree(pr.destDir)
	if err != nil {
		_ = removeAllContained(m.cacheDir, pr.destDir)
		_ = removeContained(m.cacheDir, manifestPath)
		return fmt.Errorf("hashing extracted template: %w", err)
	}
	if err := writeCacheManifest(manifestPath, pr.templateName, hash, pr.version, pr.source); err != nil {
		_ = removeAllContained(m.cacheDir, pr.destDir)
		_ = removeContained(m.cacheDir, manifestPath)
		return err
	}

	return nil
}

// cacheManifest records the provenance of a cached template: the template name,
// a deterministic SHA-256 of the extracted tree, and the version and download
// URL it was fetched from. It lets the pull path and the resolver detect a
// corrupted, stale, or naively tampered cache directory.
//
// The hash is unkeyed, so this is integrity checking, not authentication: an
// actor who can write both the cache tree and its manifest can forge a matching
// pair. It therefore defends against accidental corruption, staleness, and
// tampering that does not also rewrite the manifest, but it is not a
// cryptographic defense against a local writer with access to the cache root.
type cacheManifest struct {
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

// cacheSourceKey hashes the download URL. Hashing avoids persisting credentials
// embedded in a URL while still changing when the version or registry changes.
func cacheSourceKey(downloadURL string) string {
	sum := sha256.Sum256([]byte(downloadURL))
	return hex.EncodeToString(sum[:])
}

// cacheIsValid reports whether pr.destDir exists, matches the manifest hash, and
// was fetched from the configured version and URL.
func cacheIsValid(pr pullRequest, manifestPath string) bool {
	if _, err := os.Stat(pr.destDir); err != nil {
		return false
	}
	mf, err := readCacheManifest(manifestPath)
	if err != nil {
		return false
	}
	if mf.Version != pr.version || mf.Source != pr.source {
		return false
	}
	return treeMatches(pr.destDir, mf)
}

// cacheManifestMatches reports whether the tree at dir hashes to the value
// recorded in the manifest at manifestPath. A missing or unreadable manifest,
// or a hash mismatch, is reported as not matching so callers fail closed.
//
// As noted on cacheManifest, the hash is unkeyed and only detects changes that
// do not also rewrite the manifest.
func cacheManifestMatches(dir, manifestPath string) bool {
	mf, err := readCacheManifest(manifestPath)
	if err != nil {
		return false
	}
	return treeMatches(dir, mf)
}

// treeMatches reports whether the regular files under dir hash to mf.Hash. An
// empty tree is never considered a match.
func treeMatches(dir string, mf *cacheManifest) bool {
	hash, err := hashTree(dir)
	if err != nil {
		return false
	}
	if hash != mf.Hash {
		return false
	}
	// An empty tree has a well-defined hash, so a manifest can record it and
	// make an empty directory look valid. That would suppress a re-pull and let
	// an empty template masquerade as a cache hit, so a tree with no regular
	// files is never considered valid.
	return treeHasRegularFile(dir)
}

// treeHasRegularFile reports whether dir contains at least one regular file at
// any depth. It mirrors hashTree's walk and treats a walk error as "no file" so
// callers fail closed.
func treeHasRegularFile(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Type().IsRegular() {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// cacheManifestValid reports whether the template directory at tplDir matches
// the hash in its sibling manifest (<tplDir>.manifest.json).
func cacheManifestValid(tplDir string) bool {
	return cacheManifestMatches(tplDir, tplDir+manifestSuffix)
}

func readCacheManifest(path string) (*cacheManifest, error) {
	//nolint:gosec // path is derived from the cache root, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var mf cacheManifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("parsing cache manifest %s: %w", path, err)
	}
	return &mf, nil
}

func writeCacheManifest(path, name, hash, version, source string) error {
	data, err := json.MarshalIndent(cacheManifest{Name: name, Hash: hash, Version: version, Source: source}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling cache manifest: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), cacheDirPerm); err != nil {
		return fmt.Errorf("creating cache manifest dir: %w", err)
	}
	if err := os.WriteFile(path, data, cacheFilePerm); err != nil {
		return fmt.Errorf("writing cache manifest: %w", err)
	}
	return nil
}

// hashTree computes a deterministic SHA-256 over the regular files under dir.
// Relative paths are slash-normalized and sorted, and each file contributes a
// length-framed record (the byte length and bytes of the relative path, then the
// byte length and bytes of the contents) so the encoding is unambiguous.
//
// Any entry that is neither a regular file nor a directory (a symlink, device,
// socket, or fifo) makes hashTree fail. This keeps verification fail-closed:
// SafeReadFile follows symlinks, so skipping one would let an attacker add a
// symlink after the manifest was written without changing the hash.
func hashTree(dir string) (string, error) {
	var rels []string
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("refusing to hash non-regular entry %s (mode %s)", path, d.Type())
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	sort.Strings(rels)

	hasher := sha256.New()
	var lenBuf [8]byte
	for _, rel := range rels {
		//nolint:gosec // path is within dir
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(rel)))
		hasher.Write(lenBuf[:])
		hasher.Write([]byte(rel))
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(data)))
		hasher.Write(lenBuf[:])
		hasher.Write(data)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func doDownload(downloadURL, token, name string) (*http.Response, error) {
	// The request uses a never-canceled background context; the client timeout
	// below bounds the whole exchange. The caller reads resp.Body after this
	// function returns, so the request context must not be canceled here.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, downloadURL, nil)
	if err != nil {
		redactURLError(err)
		return nil, fmt.Errorf("creating request for %s: %w", RedactURL(downloadURL), err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// Bound the whole request (connect, redirects, and body read) with the
	// client timeout.
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		redactURLError(err)
		return nil, fmt.Errorf("downloading from %q: %w", RedactURL(downloadURL), err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("downloading %q: %s", name, resp.Status)
	}

	return resp, nil
}

// redactURLError rewrites the URL carried by a *url.Error to its redacted form
// so credentials cannot leak through a wrapped request or parse error. url.Error
// renders its URL with %q, so replacing the raw string literally would miss
// escaped control characters; mutating the parsed URL is exact.
func redactURLError(err error) {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		urlErr.URL = RedactURL(urlErr.URL)
	}
}

func extractToCache(body io.Reader, cacheDir, destDir, subdirectory string) error {
	if err := validateSubdirectory(subdirectory); err != nil {
		return err
	}
	// Fail closed before any RemoveAll/Rename: destDir must be strictly inside
	// the cache root.
	if err := ensureStrictlyWithin(cacheDir, destDir); err != nil {
		return err
	}
	// Stage in an unpredictably named sibling so an existing path is never
	// reused.
	tmpDir, err := createStagingDir(cacheDir, destDir)
	if err != nil {
		return err
	}
	sourceDir, err := stageArchive(body, cacheDir, tmpDir, subdirectory)
	if err != nil {
		return err
	}
	return swapIntoCache(cacheDir, destDir, tmpDir, sourceDir)
}

// createStagingDir creates an unpredictably named staging directory as a sibling
// of destDir. The old fixed "<destDir>.tmp" name was predictable, so another
// process could occupy it first; os.MkdirTemp creates a fresh directory under a
// random name. Its 0700 mode is kept (the cache is per-user, and chmod would
// widen the window for a swapped symlink), and containment is re-checked before
// anything is staged.
func createStagingDir(cacheDir, destDir string) (string, error) {
	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, cacheDirPerm); err != nil {
		return "", fmt.Errorf("creating staging parent: %w", err)
	}
	// Re-check after creating the parent: a symlinked <cache>/<registry> must
	// not let the staging directory escape the cache root.
	if err := ensureStrictlyWithin(cacheDir, destDir); err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp(parent, stagingDirPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("creating staging dir: %w", err)
	}
	return tmpDir, nil
}

// stageArchive extracts the archive into the staging directory and validates the
// staged tree, returning the directory to promote. The existing cache is not
// touched, so a malformed archive cannot destroy a previously valid cache.
func stageArchive(body io.Reader, cacheDir, tmpDir, subdirectory string) (string, error) {
	limited := io.LimitReader(body, maxArchiveBytes)
	if err := extractTarGz(limited, tmpDir); err != nil {
		_ = removeAllContained(cacheDir, tmpDir)
		return "", fmt.Errorf("extracting archive: %w", err)
	}

	sourceDir := promoteSubdirectory(tmpDir, subdirectory)

	// The promoted source must stay within the temp dir; a value that escaped
	// (for example via "..") would otherwise be renamed into the cache.
	if err := ensureWithin(tmpDir, sourceDir); err != nil {
		_ = removeAllContained(cacheDir, tmpDir)
		return "", err
	}

	// Validate the staged tree before touching the existing cache so an empty
	// or malformed archive cannot destroy a previously valid cache.
	if err := validateTemplateCache(sourceDir); err != nil {
		_ = removeAllContained(cacheDir, tmpDir)
		return "", err
	}
	return sourceDir, nil
}

// swapIntoCache replaces the existing cache with the staged source directory.
// It runs only after extraction and validation have fully succeeded, so a
// failure before this point leaves any previous cache (and its manifest)
// untouched.
//
// The body read in stageArchive can block for as long as the download takes, so
// the containment checks performed at the top of extractToCache can be stale by
// the time the cache is swapped. In particular, a concurrent attacker can
// replace <cache>/<registry> with a symlink pointing outside the cache after
// those checks pass; the destructive operations below would then follow it.
// Re-check containment immediately before each one so a path that now escapes
// is refused (fail closed) rather than acted on. This narrows the race to the
// instant between the check and the syscall.
func swapIntoCache(cacheDir, destDir, tmpDir, sourceDir string) error {
	if err := os.MkdirAll(filepath.Dir(destDir), cacheDirPerm); err != nil {
		_ = removeAllContained(cacheDir, tmpDir)
		return fmt.Errorf("creating parent dir: %w", err)
	}

	if err := removeAllContained(cacheDir, destDir); err != nil {
		_ = removeAllContained(cacheDir, tmpDir)
		return fmt.Errorf("removing stale cache: %w", err)
	}
	if err := ensureWithin(tmpDir, sourceDir); err != nil {
		_ = removeAllContained(cacheDir, tmpDir)
		return err
	}
	if err := ensureStrictlyWithin(cacheDir, destDir); err != nil {
		_ = removeAllContained(cacheDir, tmpDir)
		return err
	}
	if err := os.Rename(sourceDir, destDir); err != nil {
		_ = removeAllContained(cacheDir, tmpDir)
		return fmt.Errorf("moving to cache: %w", err)
	}

	if sourceDir != tmpDir {
		_ = removeAllContained(cacheDir, tmpDir)
	}

	return nil
}

// removeAllContained re-checks that target is strictly inside base, then removes
// it. The containment check is performed immediately before the removal so a
// directory that a concurrent actor swapped for an escaping symlink after an
// earlier check is refused (fail closed) instead of being followed and deleted
// outside base. The remaining window is the instant between the check and the
// syscall.
func removeAllContained(base, target string) error {
	if err := ensureStrictlyWithin(base, target); err != nil {
		return err
	}
	return os.RemoveAll(target)
}

// removeContained is removeAllContained for a single file.
func removeContained(base, target string) error {
	if err := ensureStrictlyWithin(base, target); err != nil {
		return err
	}
	return os.Remove(target)
}

func promoteSubdirectory(tmpDir, subdirectory string) string {
	if subdirectory == "" {
		return tmpDir
	}
	subDir := filepath.Join(tmpDir, subdirectory)
	// Never promote a directory that escaped the temp dir; fall back to the
	// whole archive instead.
	if ensureWithin(tmpDir, subDir) != nil {
		return tmpDir
	}
	if info, err := os.Stat(subDir); err == nil && info.IsDir() {
		return subDir
	}
	return tmpDir
}

// ensureStrictlyWithin returns an error unless target is strictly inside base
// after resolving symlinks. It is used before destructive operations so that a
// bad name, or a symlinked directory inside the cache, can never reach outside
// the template cache.
func ensureStrictlyWithin(base, target string) error {
	return ensureContained(base, target, false)
}

// ensureWithin returns an error unless target is base itself or strictly inside
// base after resolving symlinks. It is used to confirm a promoted subdirectory
// did not escape its temp dir (the whole-archive case returns base unchanged).
func ensureWithin(base, target string) error {
	return ensureContained(base, target, true)
}

// ensureContained resolves symlinks in base and target and reports whether the
// resolved target stays inside the resolved base. allowEqual permits the two to
// resolve to the same path (the whole-archive promotion case); otherwise the
// target must be strictly inside base. Every failure mode is treated as an
// error (fail closed) so a path that cannot be resolved is never trusted.
func ensureContained(base, target string, allowEqual bool) error {
	resolvedBase, err := resolveExistingPrefix(base)
	if err != nil {
		return fmt.Errorf("resolving base path %q: %w", base, err)
	}
	resolvedTarget, err := resolveExistingPrefix(target)
	if err != nil {
		return fmt.Errorf("resolving path %q: %w", target, err)
	}
	rel, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil {
		return fmt.Errorf("resolving %q relative to %q: %w", target, base, err)
	}
	if filepath.IsAbs(rel) || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		(!allowEqual && rel == ".") {
		return fmt.Errorf("refusing to use path %q outside %q", target, base)
	}
	return nil
}

// resolveExistingPrefix returns target with every symlink in its longest
// existing prefix resolved. The non-existent tail (which cannot contain a
// symlink yet) is appended lexically to the resolved prefix. It fails closed:
// if no prefix can be resolved, or a component is not a directory, it returns
// an error instead of a path that might escape containment.
func resolveExistingPrefix(target string) (string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	cur := filepath.Clean(abs)
	remainder := ""
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			if remainder == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, remainder), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Walked up to the filesystem root without resolving anything.
			return "", err
		}
		remainder = filepath.Join(filepath.Base(cur), remainder)
		cur = parent
	}
}

// validateTemplateCache checks that a cached template directory contains at
// least one file (non-empty template).
func validateTemplateCache(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading template cache: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			return nil
		}
		// Check subdirectories too (e.g., templates/ dir wrapping)
		subEntries, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				return nil
			}
		}
	}
	return errors.New("template archive is empty or has no files")
}

func extractTarGz(r io.Reader, destDir string) error {
	return fileutil.ExtractTarGz(r, destDir, maxArchiveBytes)
}
