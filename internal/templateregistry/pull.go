package templateregistry

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	httpTimeout    = 60 * time.Second
	maxArchiveBytes = 10 << 20

	cacheDirPerm  os.FileMode = 0750
	cacheFilePerm os.FileMode = 0640
)

// Pull downloads a template from a registry URL and caches it locally.
func (m *Manager) Pull(registryName, templateName string) error {
	m.mu.Lock()
	registries := make([]Registry, len(m.config.Registries))
	copy(registries, m.config.Registries)
	m.mu.Unlock()

	var registryURL string
	for _, r := range registries {
		if r.Name == registryName {
			registryURL = r.URL
			break
		}
	}
	if registryURL == "" {
		return fmt.Errorf("registry %q not found", registryName)
	}

	destDir := TemplateCachePath(m.cacheDir, registryName, templateName)
	if _, err := os.Stat(destDir); err == nil {
		return nil
	}

	downloadURL := fmt.Sprintf("%s/%s.tar.gz", strings.TrimRight(registryURL, "/"), templateName)

	return m.pullFromURL(destDir, downloadURL)
}

// PullFromURL downloads a template archive from an arbitrary URL and caches it.
func (m *Manager) PullFromURL(registryName, templateName, downloadURL string) error {
	destDir := TemplateCachePath(m.cacheDir, registryName, templateName)
	if _, err := os.Stat(destDir); err == nil {
		return nil
	}

	return m.pullFromURL(destDir, downloadURL)
}

func (m *Manager) pullFromURL(destDir, url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading from %q: %w", url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading: %s", resp.Status)
	}

	limited := io.LimitReader(resp.Body, maxArchiveBytes)
	if err := extractTarGz(limited, destDir); err != nil {
		return fmt.Errorf("extracting: %w", err)
	}

	return nil
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
			//nolint:gosec // Limited to maxArchiveBytes, DOG not feasible
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("writing file %s: %w", target, err)
			}
			_ = f.Close()
		}
	}

	return nil
}
