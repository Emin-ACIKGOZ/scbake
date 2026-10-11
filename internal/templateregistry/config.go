// Package templateregistry manages remote template registries and the
// resolution chain that checks registry caches before falling back to
// embedded templates.
package templateregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// credentialQueryMarkers are case-insensitive substrings that, when present in
// a query parameter name, mark its value as credential-bearing. Substring
// matching (rather than an exact allowlist) also covers vendor-specific names
// such as "X-Amz-Signature", "sig", and "key".
var credentialQueryMarkers = []string{
	"token", "secret", "sig", "key", "auth", "pass", "cred",
	"code", "session", "jwt", "sas",
}

// isCredentialQueryKey reports whether a query parameter name looks like it
// carries a credential.
func isCredentialQueryKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range credentialQueryMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// RedactURL strips credentials from a registry URL so it is safe to print in
// output or embed in error messages. It removes userinfo (username/password),
// the fragment (which can carry OAuth implicit-flow tokens), and any
// credential-bearing query parameter value. Unparseable URLs are replaced with
// a safe placeholder rather than leaking the raw string.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid registry URL]"
	}

	u.User = nil
	u.Fragment = ""
	u.RawFragment = ""

	if u.RawQuery != "" {
		query, parseErr := url.ParseQuery(u.RawQuery)
		if parseErr != nil {
			// Fail closed: a malformed query (for example ';' separators or an
			// invalid escape) makes ParseQuery drop or misparse parameters, so
			// withhold the whole query rather than risk echoing a credential.
			u.RawQuery = "REDACTED"
		} else {
			changed := false
			for key := range query {
				if isCredentialQueryKey(key) {
					query.Set(key, "REDACTED")
					changed = true
				}
			}
			if changed {
				u.RawQuery = query.Encode()
			}
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

// ValidateName reports whether name is safe to use as a single path segment for
// a registry, a template, or a project directory. It rejects empty names, the
// special entries "." and "..", anything containing a path separator or volume
// syntax, and control characters. Rejecting these is required so that
// filepath.Join(cacheDir, registryName, templateName) can never escape the
// template cache.
func ValidateName(name string) error {
	if err := validateNameForm(name); err != nil {
		return err
	}
	if err := validateNamePathSafety(name); err != nil {
		return err
	}
	return validateNameRunes(name)
}

// validateNameForm rejects names whose shape makes them unsafe as a single path
// segment: empty, the special entries "." and "..", or anything containing a
// path separator or drive syntax.
func validateNameForm(name string) error {
	if name == "" {
		return errors.New("name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name %q is a reserved path segment", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name %q must not contain a path separator", name)
	}
	// A colon is not a separator on Unix, but it introduces Windows drive
	// syntax and alternate data streams, so reject it on every platform.
	if strings.ContainsRune(name, ':') {
		return fmt.Errorf("name %q must not contain ':'", name)
	}
	return nil
}

// validateNamePathSafety rejects names that resolve outside the intended single
// path segment: absolute or volume-qualified names, trailing dots or spaces
// (which Windows strips), and Windows-reserved device names.
func validateNamePathSafety(name string) error {
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return fmt.Errorf("name %q must not be absolute", name)
	}
	// Windows silently strips trailing dots and spaces, so ".." spelled with a
	// trailing space would collapse to a traversal segment; reject them.
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("name %q must not end with a dot or space", name)
	}
	if isReservedWindowsName(name) {
		return fmt.Errorf("name %q is a reserved device name", name)
	}
	return nil
}

// validateNameRunes rejects names containing control characters.
func validateNameRunes(name string) error {
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("name %q must not contain control characters", name)
		}
	}
	return nil
}

// windowsReservedNames are the classic DOS device names that Windows treats as
// special, so a path segment equal to one of them cannot name an ordinary file
// or directory. The COM1-COM9 and LPT1-LPT9 ranges are handled separately.
var windowsReservedNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
}

// isReservedWindowsName reports whether segment (case-insensitively) names a
// Windows reserved device, so a cache path built from it cannot be created on
// Windows (or could resolve to a device rather than a file).
func isReservedWindowsName(segment string) bool {
	upper := strings.ToUpper(segment)
	if _, ok := windowsReservedNames[upper]; ok {
		return true
	}
	if len(upper) == 4 &&
		(strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) &&
		upper[3] >= '1' && upper[3] <= '9' {
		return true
	}
	return false
}

// validateSubdirectory rejects a registry subdirectory that is absolute or that
// contains a ".." segment. Such a value would make promoteSubdirectory return a
// directory outside the extraction temp dir, which the pull path would then
// rename into the cache. Each segment is normalized (Windows strips trailing
// dots and spaces) before the traversal check so ".. " cannot slip through.
func validateSubdirectory(subdirectory string) error {
	if subdirectory == "" {
		return nil
	}
	if isAbsoluteOrRooted(subdirectory) {
		return fmt.Errorf("subdirectory %q must be relative", subdirectory)
	}
	for _, r := range subdirectory {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("subdirectory %q must not contain control characters", subdirectory)
		}
	}
	for _, raw := range strings.FieldsFunc(subdirectory, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if err := validateSubdirectorySegment(subdirectory, raw); err != nil {
			return err
		}
	}
	return nil
}

// validateSubdirectorySegment rejects a single subdirectory segment that would
// traverse upward or name a Windows-reserved device.
// isAbsoluteOrRooted reports whether p is absolute or rooted (leading separator).
// It also catches a leading "/" on Windows, where filepath.IsAbs does not treat
// such a path as absolute.
func isAbsoluteOrRooted(p string) bool {
	return filepath.IsAbs(p) || filepath.VolumeName(p) != "" ||
		strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`)
}

func validateSubdirectorySegment(subdirectory, raw string) error {
	// Windows strips trailing dots and spaces from a path component, so a
	// segment written as ".. " (or "..") can collapse to a traversal
	// segment. Reject both the raw and space/dot-stripped forms.
	if raw == ".." || strings.TrimRight(raw, " ") == ".." ||
		strings.TrimRight(raw, ".") == ".." {
		return fmt.Errorf("subdirectory %q must not contain %q", subdirectory, "..")
	}
	if isReservedWindowsName(raw) || isReservedWindowsName(strings.TrimRight(raw, " .")) {
		return fmt.Errorf("subdirectory %q contains a reserved device name", subdirectory)
	}
	return nil
}

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
	// Best effort: record the registry insertion order next to the cache so the
	// resolver can apply an explicit, deterministic precedence. If this cannot
	// be written, resolution falls back to a sorted order rather than failing
	// the config change.
	_ = m.writeRegistryOrder()
	return nil
}

// registryOrderFileName is the file, stored at the root of the template cache,
// that records the order in which registries were added. The resolver reads it
// to give the first-added registry precedence when several cached registries
// provide the same template.
const registryOrderFileName = ".registry-order.json"

// registryOrder is the on-disk representation of the registry precedence order.
type registryOrder struct {
	Registries []string `json:"registries"`
}

// writeRegistryOrder persists the current registry insertion order next to the
// cache. It is best effort; callers ignore the error so a cache-side write
// failure never blocks a config change.
func (m *Manager) writeRegistryOrder() error {
	if m.cacheDir == "" {
		return nil
	}
	names := make([]string, 0, len(m.config.Registries))
	for _, r := range m.config.Registries {
		names = append(names, r.Name)
	}
	data, err := json.MarshalIndent(registryOrder{Registries: names}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.cacheDir, cacheDirPerm); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.cacheDir, registryOrderFileName), data, cacheFilePerm)
}

// readRegistryOrder loads the recorded registry precedence order, or nil when it
// is absent or unreadable.
func readRegistryOrder(cacheDir string) []string {
	//nolint:gosec // path is derived from the cache root, not user input
	data, err := os.ReadFile(filepath.Join(cacheDir, registryOrderFileName))
	if err != nil {
		return nil
	}
	var o registryOrder
	if err := json.Unmarshal(data, &o); err != nil {
		return nil
	}
	return o.Registries
}

// orderRegistries returns registry directory names in precedence order: the
// order the registries were added, as recorded in the cache's order file.
// Directories absent from the order file are appended in lexical order so the
// result is always deterministic. When no order file exists it falls back to
// lexical order.
func orderRegistries(cacheDir string, dirs []string) []string {
	order := readRegistryOrder(cacheDir)
	if len(order) == 0 {
		sort.Strings(dirs)
		return dirs
	}
	rank := make(map[string]int, len(order))
	for i, name := range order {
		if _, seen := rank[name]; !seen {
			rank[name] = i
		}
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		ri, iKnown := rank[dirs[i]]
		rj, jKnown := rank[dirs[j]]
		switch {
		case iKnown && jKnown:
			return ri < rj
		case iKnown:
			return true
		case jKnown:
			return false
		default:
			return dirs[i] < dirs[j]
		}
	})
	return dirs
}

// Add registers a new registry with optional token, version, and subdirectory.
func (m *Manager) Add(name, url, token, version, subdirectory string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ValidateName(name); err != nil {
		return fmt.Errorf("invalid registry name: %w", err)
	}
	if err := validateSubdirectory(subdirectory); err != nil {
		return err
	}

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

// List returns a copy of all registries in the order they were added. That
// insertion order is the resolution precedence: when more than one cached
// registry provides the same template, the earliest-added one wins.
func (m *Manager) List() []Registry {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Registry, len(m.config.Registries))
	copy(out, m.config.Registries)
	return out
}

// CacheDir returns the root cache directory for pulled templates.
func (m *Manager) CacheDir() string {
	return m.cacheDir
}

// TemplateCachePath returns the directory where a specific template from
// a registry would be cached: <cacheDir>/<registry>/<template>/
//
// It returns the empty string when either name is not a safe single path
// segment, so a traversal name can never produce a path outside cacheDir.
func TemplateCachePath(cacheDir, registryName, templateName string) string {
	if ValidateName(registryName) != nil || ValidateName(templateName) != nil {
		return ""
	}
	return filepath.Join(cacheDir, registryName, templateName)
}

// manifestSuffix is appended to a template cache directory to form the path of
// the integrity manifest that accompanies it.
const manifestSuffix = ".manifest.json"

// TemplateManifestPath returns the path of the integrity manifest recorded for
// a cached template. The manifest is a sibling of the template directory
// (never inside it) so its own bytes are excluded from the tree hash:
// <cacheDir>/<registry>/<template>.manifest.json
//
// It returns the empty string when either name is not a safe single path
// segment (see TemplateCachePath).
func TemplateManifestPath(cacheDir, registryName, templateName string) string {
	base := TemplateCachePath(cacheDir, registryName, templateName)
	if base == "" {
		return ""
	}
	return base + manifestSuffix
}
