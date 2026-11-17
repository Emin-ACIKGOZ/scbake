// Package templateregistry manages remote template registries and the
// resolution chain that checks registry caches before falling back to
// embedded templates.
package templateregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
	// Best effort: record the registry insertion order next to the cache for
	// older binaries that predate config-derived membership and still read it.
	// The current resolver ignores this file and follows the running process's
	// configured order. If writing fails, the config change still succeeds.
	_ = m.writeRegistryOrder()
	return nil
}

// registryOrderFileName is the file, stored at the root of the template cache,
// that records the order in which registries were added. It is retained for
// mixed-version compatibility: older binaries use it for precedence, but the
// current resolver derives order from the configured registries slice instead.
const registryOrderFileName = ".registry-order.json"

// registryOrder is the on-disk representation of the registry precedence order.
type registryOrder struct {
	Registries []string `json:"registries"`
}

// writeRegistryOrder persists the current registry insertion order next to the
// cache. It is best effort; callers ignore the error so a cache-side write
// failure never blocks a config change. Only older binaries read it.
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
// is absent or unreadable. The current resolver no longer consults it; it is
// kept for mixed-version compatibility and diagnostics.
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

	// Reclaim attributed staging residue before appending. The removal of any
	// matching name has already been saved, so the sweep deletes rather than
	// restores it; running before the duplicate check keeps a rejected add from
	// mutating the cache.
	m.sweepEvictedCaches(time.Now())

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

	// A token rotation may be the first mutation after a crash, so reclaim
	// attributed residue before looking the registry up.
	m.sweepEvictedCaches(time.Now())

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
//
// The cached directory is first renamed aside inside the cache root, then the
// config change is saved, and only then is the staging directory deleted. If
// saving fails, both the in-memory config and the cache directory are restored
// and the combined error is returned. This keeps a failed removal from leaving
// the config listing a registry whose cache has already been deleted (or a
// removed registry whose cache is still live in the common case).
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reclaim attributed residue first. On a retry after a post-save cleanup
	// failure this deletes the old staged tree before the not-found check.
	m.sweepEvictedCaches(time.Now())

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

	staged, err := m.stageRegistryCache(name)
	if err != nil {
		return err
	}

	// Build a new slice instead of appending into the old backing array, so the
	// previous config (restored on a save failure) is not mutated in place.
	registries := make([]Registry, 0, len(m.config.Registries)-1)
	registries = append(registries, m.config.Registries[:idx]...)
	registries = append(registries, m.config.Registries[idx+1:]...)
	previous := m.config
	m.config = Config{Registries: registries}

	if err := m.save(); err != nil {
		m.config = previous
		if restoreErr := m.restoreRegistryCache(staged); restoreErr != nil {
			return fmt.Errorf("%w; restoring cached templates for registry %q also failed: %v",
				err, name, restoreErr)
		}
		return err
	}

	if err := m.discardRegistryCache(staged); err != nil {
		return fmt.Errorf("saved registry config but failed to delete evicted cache for registry %q: %w", name, err)
	}
	return nil
}

// evictionStagePrefix names the temporary sibling directory that holds a cache
// entry between the rename-aside and the config save. It stays inside the cache
// root, so the rename never crosses a filesystem (or volume on Windows).
const evictionStagePrefix = ".evict-"

const (
	// evictionKeyBytes is the truncated SHA-256 length embedded in a staging
	// path. 48 bits makes an accidental collision negligible; an attacker who
	// can plant a staging root can already write the cache itself.
	evictionKeyBytes = 6
	// evictionKeyHexLen is the length of the hex-encoded key in a staging name.
	evictionKeyHexLen = 2 * evictionKeyBytes
	// evictionSweepGrace protects a staging root that another process has just
	// created but not yet renamed or saved over. A normal stage-to-save window
	// is milliseconds, so a minute only delays reclaiming abandoned residue.
	evictionSweepGrace = time.Minute
)

// evictionKey derives the short key embedded in a staging path. It binds a
// staged tree to the configured registry name and source: a re-add under a
// different name, URL, version, or subdirectory yields a different key, so the
// sweep deletes the old tree instead of restoring it.
//
// Each field is length-prefixed so no value can shift the boundary between
// fields, and the field order is a stability contract with roots already on
// disk. The token is deliberately excluded: it is an Authorization header, not
// part of the download URL, and the pull path also reuses a cache across a token
// rotation.
func evictionKey(reg Registry) string {
	encoded := fmt.Sprintf("%d:%s|%d:%s|%d:%s|%d:%s",
		len(reg.Name), reg.Name,
		len(reg.URL), reg.URL,
		len(reg.Version), reg.Version,
		len(reg.Subdirectory), reg.Subdirectory,
	)
	sum := sha256.Sum256([]byte(encoded))
	return hex.EncodeToString(sum[:evictionKeyBytes])
}

// stagedRegistryCache records a cache directory that was renamed aside while the
// config change is saved. staging is empty when there was nothing to evict.
type stagedRegistryCache struct {
	original string
	staging  string
}

// stageRegistryCache renames <cacheDir>/<name> to a keyed sibling
// .evict-<key>-<random> path inside the cache root. It is a no-op when no cache
// is configured, nothing is cached, the name is invalid (an invalid name can
// never be resolved, so its residue is inert), or the target is missing.
//
// A symlink at <cacheDir>/<name> is renamed as a link; its target is never
// followed. The containment check is lexical (see ensureLexicallyWithin) so the
// link itself is moved rather than refused as if it pointed outside.
func (m *Manager) stageRegistryCache(name string) (stagedRegistryCache, error) {
	if !evictableCacheEntry(m.cacheDir, name) {
		return stagedRegistryCache{}, nil
	}

	target := filepath.Join(m.cacheDir, name)
	if err := ensureLexicallyWithin(m.cacheDir, target); err != nil {
		return stagedRegistryCache{}, fmt.Errorf("evicting cached templates for registry %q: %w", name, err)
	}
	if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
		return stagedRegistryCache{}, nil
	} else if err != nil {
		return stagedRegistryCache{}, fmt.Errorf("evicting cached templates for registry %q: %w", name, err)
	}

	reg, ok := m.firstRegistry(name)
	if !ok {
		return stagedRegistryCache{}, fmt.Errorf("evicting cached templates for registry %q: registry is not configured", name)
	}

	staging, err := reserveEvictionPath(m.cacheDir, evictionKey(reg))
	if err != nil {
		return stagedRegistryCache{}, fmt.Errorf("evicting cached templates for registry %q: %w", name, err)
	}
	if err := os.Rename(target, staging); err != nil {
		return stagedRegistryCache{}, fmt.Errorf("evicting cached templates for registry %q: %w", name, err)
	}
	return stagedRegistryCache{original: target, staging: staging}, nil
}

// firstRegistry returns the first configured registry with the given name. The
// resolver gives the earliest match precedence, so the sweep must attribute a
// staged tree to the same registry.
func (m *Manager) firstRegistry(name string) (Registry, bool) {
	for _, r := range m.config.Registries {
		if r.Name == name {
			return r, true
		}
	}
	return Registry{}, false
}

// evictableCacheEntry reports whether a cache entry could exist for name. An
// invalid name is not evictable because it can never be resolved.
func evictableCacheEntry(cacheDir, name string) bool {
	return cacheDir != "" && ValidateName(name) == nil
}

// reserveEvictionPath returns an unused path inside cacheDir for a staged cache
// entry. MkdirTemp picks the unique name atomically; the empty directory is then
// removed so the path can receive the rename. All staging stays on the same
// volume as <cacheDir>/<name>, so the rename is a same-parent move.
func reserveEvictionPath(cacheDir, key string) (string, error) {
	dir, err := os.MkdirTemp(cacheDir, evictionStagePrefix+key+"-")
	if err != nil {
		return "", fmt.Errorf("creating eviction staging path: %w", err)
	}
	if err := os.Remove(dir); err != nil {
		return "", fmt.Errorf("preparing eviction staging path: %w", err)
	}
	return dir, nil
}

// restoreRegistryCache moves a staged cache entry back after a failed save. It
// is a no-op when nothing was staged.
func (m *Manager) restoreRegistryCache(staged stagedRegistryCache) error {
	if staged.staging == "" {
		return nil
	}
	if err := ensureLexicallyWithin(m.cacheDir, staged.staging); err != nil {
		return err
	}
	if err := ensureLexicallyWithin(m.cacheDir, staged.original); err != nil {
		return err
	}
	return os.Rename(staged.staging, staged.original)
}

// discardRegistryCache deletes a staged cache entry after the config change is
// durable. It is a no-op when nothing was staged. A staged symlink is unlinked
// without following it; a staged directory tree is removed recursively.
func (m *Manager) discardRegistryCache(staged stagedRegistryCache) error {
	if staged.staging == "" {
		return nil
	}
	if err := ensureLexicallyWithin(m.cacheDir, staged.staging); err != nil {
		return err
	}
	info, err := os.Lstat(staged.staging)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(staged.staging)
	}
	return os.RemoveAll(staged.staging)
}

// parseEvictionRoot parses a keyed staging basename this version writes:
// ".evict-" followed by evictionKeyHexLen lowercase hex digits, "-", and one or
// more ASCII digits. Anything else (including the legacy random ".evict-<digits>"
// roots) is unattributable and is left alone.
func parseEvictionRoot(base string) (key string, ok bool) {
	rest, found := strings.CutPrefix(base, evictionStagePrefix)
	if !found || len(rest) < evictionKeyHexLen+2 {
		return "", false
	}
	key = rest[:evictionKeyHexLen]
	if rest[evictionKeyHexLen] != '-' {
		return "", false
	}
	if !isLowerHex(key) || !isASCIIDigits(rest[evictionKeyHexLen+1:]) {
		return "", false
	}
	return key, true
}

// isLowerHex reports whether s is non-empty lowercase hexadecimal.
func isLowerHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// isASCIIDigits reports whether s is non-empty and all ASCII digits.
func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// sweepEvictedCaches reclaims attributed staging residue left by an interrupted
// or failed removal. It runs at the start of every config mutation, under m.mu,
// so the configured set it reads matches the set the mutation is about to
// change.
//
// A staged root is restored only when its key still maps to a configured,
// validly named registry and that registry's live path is absent. Every other
// parseable root is deleted. The sweep is best effort: any error (an unreadable
// cache, a race with another process, a permission failure) leaves the entry for
// a later sweep and never aborts the mutation. Legacy random roots, configured
// registries whose names begin with the staging prefix, and roots younger than
// the grace period are never touched.
func (m *Manager) sweepEvictedCaches(now time.Time) {
	if m.cacheDir == "" {
		return
	}
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		// A missing or unreadable cache has nothing to reclaim.
		return
	}

	// Skip any entry whose name is a configured registry: a live registry may
	// legitimately be named ".evict-..." and must never be swept.
	configuredNames := make(map[string]struct{}, len(m.config.Registries))
	// Attribute each key to the first configured registry with a valid name,
	// matching the resolver's precedence. Invalid or duplicate hand-edited
	// entries cannot produce a restore.
	byKey := make(map[string]Registry, len(m.config.Registries))
	for _, reg := range m.config.Registries {
		configuredNames[reg.Name] = struct{}{}
		if ValidateName(reg.Name) != nil {
			continue
		}
		key := evictionKey(reg)
		if _, exists := byKey[key]; !exists {
			byKey[key] = reg
		}
	}

	for _, entry := range entries {
		m.sweepEvictionRoot(now, entry, configuredNames, byKey)
	}
}

// sweepEvictionRoot applies the restore-or-discard decision to one cache entry.
// DirEntry.Info reports the entry itself (never a symlink target), so the grace
// check cannot be redirected outside the cache.
func (m *Manager) sweepEvictionRoot(now time.Time, entry os.DirEntry, configuredNames map[string]struct{}, byKey map[string]Registry) {
	base := entry.Name()
	if !strings.HasPrefix(base, evictionStagePrefix) {
		return
	}
	if _, configured := configuredNames[base]; configured {
		return
	}
	key, ok := parseEvictionRoot(base)
	if !ok {
		return
	}
	info, err := entry.Info()
	if err != nil || now.Sub(info.ModTime()) < evictionSweepGrace {
		return
	}
	root := filepath.Join(m.cacheDir, base)
	if err := ensureLexicallyWithin(m.cacheDir, root); err != nil {
		return
	}
	reg, ok := byKey[key]
	if !ok {
		// The registry is gone, renamed, or re-added from a different source:
		// the staged tree can never be served safely, so discard it.
		_ = os.RemoveAll(root)
		return
	}
	m.restoreOrDiscardEvictionRoot(root, filepath.Join(m.cacheDir, reg.Name))
}

// restoreOrDiscardEvictionRoot restores root to live when the live path is
// absent, and discards root when live exists (live wins; it is never
// overwritten). It uses Lstat only and never follows a symlink.
func (m *Manager) restoreOrDiscardEvictionRoot(root, live string) {
	if _, err := os.Lstat(live); err == nil {
		_ = os.RemoveAll(root)
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		// Cannot decide safely; keep the tree for a later sweep.
		return
	}
	if err := ensureLexicallyWithin(m.cacheDir, live); err != nil {
		return
	}
	_ = os.Rename(root, live)
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
