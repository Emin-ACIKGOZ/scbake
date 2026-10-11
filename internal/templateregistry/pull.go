package templateregistry

import (
	"context"
	"crypto/sha256"
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
	registry := m.Get(registryName)
	if registry == nil {
		return fmt.Errorf("registry %q not found", registryName)
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
	manifestPath := TemplateManifestPath(m.cacheDir, pr.registryName, pr.templateName)

	// A cache hit is only trusted when the directory exists and its contents
	// still match the recorded manifest. Anything else (missing directory,
	// missing manifest, or a hash mismatch from tampering) is treated as a miss
	// and re-downloaded. Caches written before manifests existed have no
	// manifest, so they are re-pulled once and gain one.
	if cacheIsValid(pr.destDir, manifestPath) {
		return nil
	}

	if err := os.RemoveAll(pr.destDir); err != nil {
		return fmt.Errorf("removing stale template cache: %w", err)
	}
	if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale cache manifest: %w", err)
	}

	resp, err := doDownload(pr.downloadURL, pr.token, pr.templateName)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if err := extractToCache(resp.Body, pr.destDir, pr.subdirectory); err != nil {
		return err
	}

	hash, err := hashTree(pr.destDir)
	if err != nil {
		_ = os.RemoveAll(pr.destDir)
		return fmt.Errorf("hashing extracted template: %w", err)
	}
	if err := writeCacheManifest(manifestPath, pr.templateName, hash); err != nil {
		_ = os.RemoveAll(pr.destDir)
		return err
	}

	return nil
}

// cacheManifest records the provenance of a cached template: the template name
// and a deterministic SHA-256 of the extracted tree. It lets the pull path and
// the resolver detect a poisoned or stale cache directory.
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
func cacheManifestMatches(dir, manifestPath string) bool {
	mf, err := readCacheManifest(manifestPath)
	if err != nil {
		return false
	}
	hash, err := hashTree(dir)
	if err != nil {
		return false
	}
	return hash == mf.Hash
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
// Relative paths are slash-normalized and sorted, and each file contributes
// "<relpath>\x00<bytes>" so both names and contents are covered.
func hashTree(dir string) (string, error) {
	var rels []string
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
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
	for _, rel := range rels {
		//nolint:gosec // path is within dir
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		hasher.Write([]byte(rel))
		hasher.Write([]byte{0})
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

func extractToCache(body io.Reader, destDir, subdirectory string) error {
	tmpDir := destDir + ".tmp"
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

	if err := os.MkdirAll(filepath.Dir(destDir), cacheDirPerm); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("creating parent dir: %w", err)
	}
	if err := os.Rename(sourceDir, destDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("moving to cache: %w", err)
	}

	if sourceDir != tmpDir {
		_ = os.RemoveAll(tmpDir)
	}

	if err := validateTemplateCache(destDir); err != nil {
		_ = os.RemoveAll(destDir)
		return err
	}

	return nil
}

func promoteSubdirectory(tmpDir, subdirectory string) string {
	if subdirectory == "" {
		return tmpDir
	}
	subDir := filepath.Join(tmpDir, subdirectory)
	if info, err := os.Stat(subDir); err == nil && info.IsDir() {
		return subDir
	}
	return tmpDir
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
