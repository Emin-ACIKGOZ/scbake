// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Command gen regenerates the embedded SPDX license archive and index from
// the official SPDX license-list-data repository.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	defaultTag       = "v3.29.0"
	licenseListPath  = "json/licenses.json"
	licenseDir       = "json/details"
	licenseRepoURL   = "https://raw.githubusercontent.com/spdx/license-list-data/"
	maxResponseBytes = 2 << 20
	fetchWorkers     = 16
	fileMode         = 0o644
	dirMode          = 0o755
	archiveTimestamp = "1980-01-01T00:00:00Z"
	httpTimeout      = 60 * time.Second
)

var (
	sourceDir = flag.String("source", "", "read from a local license-list-data checkout instead of downloading")
	tag       = flag.String("tag", defaultTag, "SPDX license-list-data tag to download")
	outPath   = flag.String("out", "licenses.zip", "output archive path")
	indexPath = flag.String("index", "index.go", "output Go index path")
)

type licenseList struct {
	Version  string `json:"licenseListVersion"`
	Licenses []struct {
		ID string `json:"licenseId"`
	} `json:"licenses"`
}

type licenseDetail struct {
	ID   string `json:"licenseId"`
	Text string `json:"licenseText"`
}

type loader struct {
	sourceDir string
	baseURL   string
	client    *http.Client
}

type result struct {
	id   string
	text string
	err  error
}

func main() {
	flag.Parse()

	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	l, err := newLoader(*sourceDir, *tag)
	if err != nil {
		return err
	}

	list, err := l.loadList(ctx)
	if err != nil {
		return fmt.Errorf("load license list: %w", err)
	}
	if list.Version == "" {
		return errors.New("license list is missing licenseListVersion")
	}

	ids := make([]string, 0, len(list.Licenses))
	for _, license := range list.Licenses {
		if license.ID == "" {
			return errors.New("license list contains an empty licenseId")
		}
		ids = append(ids, license.ID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)

	texts, err := l.loadTexts(ctx, ids)
	if err != nil {
		return fmt.Errorf("load license texts: %w", err)
	}

	if err := writeArchive(*outPath, ids, texts, list.Version); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	if err := writeIndex(*indexPath, list.Version, ids); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	fmt.Printf("embedded %d SPDX licenses from %s\n", len(ids), list.Version)

	return nil
}

func newLoader(source, versionTag string) (*loader, error) {
	if source != "" {
		return &loader{sourceDir: source}, nil
	}

	if !strings.HasPrefix(versionTag, "v") || strings.ContainsAny(versionTag, "/\\?#") {
		return nil, fmt.Errorf("invalid SPDX tag %q", versionTag)
	}

	return &loader{
		baseURL: licenseRepoURL + versionTag + "/",
		client: &http.Client{
			Timeout: httpTimeout,
		},
	}, nil
}

func (l *loader) loadList(ctx context.Context) (*licenseList, error) {
	data, err := l.read(ctx, licenseListPath)
	if err != nil {
		return nil, err
	}

	var list licenseList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", licenseListPath, err)
	}

	return &list, nil
}

func (l *loader) loadTexts(ctx context.Context, ids []string) (map[string]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	return collectTexts(l.fetchAll(ctx, cancel, ids), len(ids))
}

func (l *loader) fetchAll(ctx context.Context, cancel context.CancelFunc, ids []string) <-chan result {
	jobs := make(chan string)
	results := make(chan result)

	var wg sync.WaitGroup
	for range fetchWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				text, err := l.loadText(ctx, id)
				results <- result{id: id, text: text, err: err}
				if err != nil {
					cancel()
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, id := range ids {
			select {
			case jobs <- id:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

func collectTexts(results <-chan result, want int) (map[string]string, error) {
	texts := make(map[string]string, want)
	var firstErr error
	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		texts[res.id] = res.text
	}

	if firstErr != nil {
		return nil, firstErr
	}
	if len(texts) != want {
		return nil, fmt.Errorf("loaded %d of %d license texts", len(texts), want)
	}

	return texts, nil
}

func (l *loader) loadText(ctx context.Context, id string) (string, error) {
	data, err := l.read(ctx, licenseDir+"/"+url.PathEscape(id)+".json")
	if err != nil {
		return "", err
	}

	var detail licenseDetail
	if err := json.Unmarshal(data, &detail); err != nil {
		return "", fmt.Errorf("parse license %q: %w", id, err)
	}
	if detail.ID != "" && detail.ID != id {
		return "", fmt.Errorf("license %q returned details for %q", id, detail.ID)
	}
	if strings.TrimSpace(detail.Text) == "" {
		return "", fmt.Errorf("license %q has no licenseText", id)
	}

	return detail.Text, nil
}

func (l *loader) read(ctx context.Context, path string) ([]byte, error) {
	if l.sourceDir != "" {
		data, err := os.ReadFile(filepath.Join(l.sourceDir, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		return data, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "scbake-spdx-generator")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", req.URL, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	if len(data) == maxResponseBytes {
		return nil, fmt.Errorf("%s: response exceeds %d bytes", req.URL, maxResponseBytes)
	}

	return data, nil
}

func writeArchive(path string, ids []string, texts map[string]string, version string) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	modified, err := time.Parse(time.RFC3339, archiveTimestamp)
	if err != nil {
		return err
	}

	for _, id := range ids {
		header := &zip.FileHeader{
			Name:     "licenses/" + id + ".txt",
			Method:   zip.Deflate,
			Modified: modified,
		}
		header.SetMode(fileMode)

		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(writer, texts[id]); err != nil {
			return err
		}
	}

	if err := zw.Close(); err != nil {
		return err
	}
	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		return err
	}

	fmt.Printf("wrote %s (%d bytes) for SPDX %s\n", path, buf.Len(), version)

	return nil
}

func writeIndex(path, version string, ids []string) error {
	var buf bytes.Buffer
	buf.WriteString("// Copyright 2025 Emin Salih Açıkgöz\n")
	buf.WriteString("// SPDX-License-Identifier: Apache-2.0\n\n")
	buf.WriteString("// Code generated by go generate; DO NOT EDIT.\n\n")
	buf.WriteString("package spdx\n\n")
	fmt.Fprintf(&buf, "// Version is the SPDX License List version embedded in this package.\nconst Version = %q\n\n", version)
	buf.WriteString("// canonicalIDs maps lowercase SPDX identifiers to their canonical casing.\n")
	buf.WriteString("var canonicalIDs = map[string]string{\n")

	seen := make(map[string]string, len(ids))
	for _, id := range ids {
		lower := strings.ToLower(id)
		if previous, ok := seen[lower]; ok {
			return fmt.Errorf("license IDs %q and %q collide case-insensitively", previous, id)
		}
		seen[lower] = id
		fmt.Fprintf(&buf, "\t%q: %q,\n", lower, id)
	}
	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}

	return writeFileAtomic(path, formatted)
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}

	file, err := os.CreateTemp(dir, ".spdx-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() {
		_ = os.Remove(name)
	}()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(fileMode); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return os.Rename(name, path)
}
