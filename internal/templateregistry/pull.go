package templateregistry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
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
		templateName: templateName,
	}
	return m.pullFromURL(pullCtx)
}

type pullRequest struct {
	downloadURL  string
	token        string
	destDir      string
	templateName string
	subdirectory string
}

func (m *Manager) pullFromURL(pr pullRequest) error {
	if _, err := os.Stat(pr.destDir); err == nil {
		return nil
	}

	resp, err := doDownload(pr.downloadURL, pr.token, pr.templateName)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	return extractToCache(resp.Body, pr.destDir, pr.subdirectory)
}

func doDownload(downloadURL, token, name string) (*http.Response, error) {
	// The request uses a never-cancelled background context; the client timeout
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
