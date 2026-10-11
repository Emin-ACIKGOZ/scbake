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
}

func (m *Manager) pullFromURL(pr pullRequest) error {
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
	// Containment is enforced again here, immediately before any destructive
	// filesystem operation, so a bad name can never reach outside the cache.
	if err := ensureStrictlyWithin(m.cacheDir, pr.destDir); err != nil {
		return err
	}
	manifestPath := TemplateManifestPath(m.cacheDir, pr.registryName, pr.templateName)
	if err := ensureStrictlyWithin(m.cacheDir, manifestPath); err != nil {
		return err
	}

	// A cache hit is only trusted when the directory exists and its contents
	// still match the recorded manifest. Anything else (missing directory,
	// missing manifest, or a hash mismatch from tampering) is treated as a miss
	// and re-downloaded. Caches written before manifests existed have no
	// manifest, so they are re-pulled once and gain one.
	if cacheIsValid(pr.destDir, manifestPath) {
		return nil
	}

	// Do not touch the existing cache or manifest yet: a transient download or
	// extraction failure must leave a previously valid cache intact.
	// extractToCache stages the download in a temp directory and only replaces
	// destDir once extraction has fully succeeded.
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
		// destDir now holds the freshly downloaded tree, not the old cache, so
		// removing it is safe; drop the manifest too so nothing points at a
		// half-written cache.
		_ = os.RemoveAll(pr.destDir)
		_ = os.Remove(manifestPath)
		return fmt.Errorf("hashing extracted template: %w", err)
	}
	if err := writeCacheManifest(manifestPath, pr.templateName, hash); err != nil {
		_ = os.RemoveAll(pr.destDir)
		_ = os.Remove(manifestPath)
		return err
	}

	return nil
}

// cacheManifest records the provenance of a cached template: the template name
// and a deterministic SHA-256 of the extracted tree. It lets the pull path and
// the resolver detect a corrupted, stale, or naively tampered cache directory.
//
// The hash is unkeyed, so this is integrity checking, not authentication: an
// actor who can write both the cache tree and its manifest can forge a matching
// pair. It therefore defends against accidental corruption, staleness, and
// tampering that does not also rewrite the manifest, but it is not a
// cryptographic defense against a local writer with access to the cache root.
type cacheManifest struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// cacheIsValid reports whether destDir exists and its contents match the hash
// recorded in the manifest at manifestPath.
func cacheIsValid(destDir, manifestPath string) bool {
	if _, err := os.Stat(destDir); err != nil {
		return false
	}
	return cacheManifestMatches(destDir, manifestPath)
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

func writeCacheManifest(path, name, hash string) error {
	data, err := json.MarshalIndent(cacheManifest{Name: name, Hash: hash}, "", "  ")
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
	// Fail closed before any RemoveAll/Rename: destDir and its staging sibling
	// must be strictly inside the cache root.
	if err := ensureStrictlyWithin(cacheDir, destDir); err != nil {
		return err
	}
	tmpDir := destDir + ".tmp"
	if err := ensureStrictlyWithin(cacheDir, tmpDir); err != nil {
		return err
	}
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("cleaning temp dir: %w", err)
	}
	if err := os.MkdirAll(tmpDir, cacheDirPerm); err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}

	limited := io.LimitReader(body, maxArchiveBytes)
	if err := extractTarGz(limited, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("extracting archive: %w", err)
	}

	sourceDir := promoteSubdirectory(tmpDir, subdirectory)

	// The promoted source must stay within the temp dir; a value that escaped
	// (for example via "..") would otherwise be renamed into the cache.
	if err := ensureWithin(tmpDir, sourceDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return err
	}

	// Validate the staged tree before touching the existing cache so an empty
	// or malformed archive cannot destroy a previously valid cache.
	if err := validateTemplateCache(sourceDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destDir), cacheDirPerm); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("creating parent dir: %w", err)
	}

	// Replace the existing cache only now that extraction and validation have
	// fully succeeded. A failure before this point leaves any previous cache
	// (and its manifest) untouched.
	if err := os.RemoveAll(destDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("removing stale cache: %w", err)
	}
	if err := os.Rename(sourceDir, destDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("moving to cache: %w", err)
	}

	if sourceDir != tmpDir {
		_ = os.RemoveAll(tmpDir)
	}

	return nil
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
