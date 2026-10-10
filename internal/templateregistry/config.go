// Package templateregistry manages remote template registries and the
// resolution chain that checks registry caches before falling back to
// embedded templates.
package templateregistry

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// credentialQueryKeys are query parameter names whose values may carry
// credentials and are redacted before a URL is printed.
var credentialQueryKeys = map[string]bool{
	"token":        true,
	"access_token": true,
	"auth":         true,
	"apikey":       true,
	"api_key":      true,
	"password":     true,
	"secret":       true,
}

// RedactURL strips userinfo (username/password) and credential-bearing query
// parameters from a registry URL so it is safe to print in output or embed in
// error messages. Unparseable URLs are replaced with a safe placeholder rather
// than leaking the raw string.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid registry URL]"
	}

	u.User = nil

	if u.RawQuery != "" {
		query := u.Query()
		changed := false
		for key := range query {
			if credentialQueryKeys[strings.ToLower(key)] {
				query.Set(key, "REDACTED")
				changed = true
			}
		}
		if changed {
			u.RawQuery = query.Encode()
		}
	}

	// A URL without a host but with an opaque component can still smuggle
	// credentials (e.g. "user:pass@host/x"); refuse to echo it.
	if u.Host == "" && u.Opaque != "" {
		return "[invalid registry URL]"
	}

	return u.String()
}

const (
	configDirName  = "scbake"
	configFileName = "registries.json"
	cacheDirName   = "scbake/templates"

	configDirPerm  os.FileMode = 0750
	configFilePerm os.FileMode = 0600
)

// Registry represents a remote template source.
type Registry struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	Token        string `json:"token,omitempty"`
	Version      string `json:"version,omitempty"`
	Subdirectory string `json:"subdirectory,omitempty"`
}

// Config manages the list of known registries.
type Config struct {
	Registries []Registry `json:"registries"`
}

// Manager handles registry config CRUD and cache paths.
type Manager struct {
	configPath string
	cacheDir   string
	mu         sync.Mutex
	config     Config
}

// NewManager creates or loads a registry manager from XDG paths.
func NewManager() (*Manager, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine user config dir: %w", err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine user cache dir: %w", err)
	}

	m := &Manager{
		configPath: filepath.Join(configDir, configDirName, configFileName),
		cacheDir:   filepath.Join(cacheDir, cacheDirName),
	}

	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// NewManagerWithPaths creates a manager with explicit paths (for testing).
func NewManagerWithPaths(configPath, cacheDir string) *Manager {
	return &Manager{
		configPath: configPath,
		cacheDir:   cacheDir,
	}
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			m.config = Config{}
			return nil
		}
		return fmt.Errorf("reading registry config: %w", err)
	}
	if err := json.Unmarshal(data, &m.config); err != nil {
		return fmt.Errorf("parsing registry config: %w", err)
	}
	return nil
}

func (m *Manager) save() error {
	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, configDirPerm); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(&m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling registry config: %w", err)
	}
	if err := os.WriteFile(m.configPath, data, configFilePerm); err != nil {
		return fmt.Errorf("writing registry config: %w", err)
	}
	return nil
}

// Add registers a new registry with optional token, version, and subdirectory.
func (m *Manager) Add(name, url, token, version, subdirectory string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, r := range m.config.Registries {
		if r.Name == name {
			return fmt.Errorf("registry %q already exists", name)
		}
	}

	m.config.Registries = append(m.config.Registries, Registry{
		Name:         name,
		URL:          url,
		Token:        token,
		Version:      version,
		Subdirectory: subdirectory,
	})
	return m.save()
}

// SetToken updates the token for an existing registry.
func (m *Manager) SetToken(name, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.config.Registries {
		if m.config.Registries[i].Name == name {
			m.config.Registries[i].Token = token
			return m.save()
		}
	}
	return fmt.Errorf("registry %q not found", name)
}

// Get returns a registry by name. Returns nil if not found.
func (m *Manager) Get(name string) *Registry {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, r := range m.config.Registries {
		if r.Name == name {
			return &Registry{
				Name:         r.Name,
				URL:          r.URL,
				Token:        r.Token,
				Version:      r.Version,
				Subdirectory: r.Subdirectory,
			}
		}
	}
	return nil
}

// Remove deletes a registry by name. Returns an error if not found.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := -1
	for i, r := range m.config.Registries {
		if r.Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("registry %q not found", name)
	}

	m.config.Registries = append(m.config.Registries[:idx], m.config.Registries[idx+1:]...)
	return m.save()
}

// List returns a sorted copy of all registries.
func (m *Manager) List() []Registry {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Registry, len(m.config.Registries))
	copy(out, m.config.Registries)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// CacheDir returns the root cache directory for pulled templates.
func (m *Manager) CacheDir() string {
	return m.cacheDir
}

// TemplateCachePath returns the directory where a specific template from
// a registry would be cached: <cacheDir>/<registry>/<template>/
func TemplateCachePath(cacheDir, registryName, templateName string) string {
	return filepath.Join(cacheDir, registryName, templateName)
}
