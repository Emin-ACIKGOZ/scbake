package templateregistry

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func doDownload(url, token, name string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading from %q: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("downloading %q: %s", name, resp.Status)
	}

	return resp, nil
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

//nolint:cyclop // Archive extraction requires multiple sequential steps
func extractTarGz(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("decompressing archive: %w", err)
	}
	defer func() {
		_ = gzr.Close()
	}()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading archive: %w", err)
		}

		name := filepath.Clean(header.Name)
		if strings.Contains(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("path traversal detected in archive: %s", header.Name)
		}

		target := filepath.Join(destDir, name)
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(filepath.Separator)) {
			return fmt.Errorf("path traversal detected: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, cacheDirPerm); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), cacheDirPerm); err != nil {
				return fmt.Errorf("creating directory for %s: %w", target, err)
			}
			//nolint:gosec // We control destDir within our cache
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, cacheFilePerm)
			if err != nil {
				return fmt.Errorf("creating file %s: %w", target, err)
			}
			//nolint:gosec // Limited to maxArchiveBytes, DOS not feasible
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("writing file %s: %w", target, err)
			}
			_ = f.Close()
		}
	}

	return nil
}
