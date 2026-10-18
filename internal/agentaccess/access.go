// Package agentaccess stores local-only agent identities and bearer credential
// verifiers. Agent records live beside node certificates, never in the git store.
package agentaccess

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"leetoffice/internal/mcp"
)

const (
	stateVersion   = 1
	maxAuditEvents = 1000
	maxStateBytes  = 16 << 20
)

// Agent is the non-secret identity shown to the local administrator.
type Agent struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Scopes      []string `json:"scopes"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Disabled    bool     `json:"disabled"`
	Revoked     bool     `json:"revoked"`
}

// Event is one local administrative change. It deliberately contains no
// credential material; the operator is always the server-designated local admin.
type Event struct {
	At      string `json:"at"`
	AgentID string `json:"agent_id"`
	Action  string `json:"action"`
	Actor   string `json:"actor"`
}

type storedAgent struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Scopes      []string `json:"scopes"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Disabled    bool     `json:"disabled"`
	Revoked     bool     `json:"revoked"`
	TokenHash   string   `json:"token_hash"`
}

type diskState struct {
	Version int           `json:"version"`
	Agents  []storedAgent `json:"agents"`
	Audit   []Event       `json:"audit"`
}

// Manager owns the credential database for a single local node.
type Manager struct {
	mu     sync.RWMutex
	path   string
	agents map[string]*storedAgent
	audit  []Event
}

// AvailableScopes returns the actual MCP tool names this build can enforce.
func AvailableScopes() []string {
	return mcp.ToolNames()
}

// Open loads or initializes the credential database. It applies private
// permissions to the identity directory and state file and rejects symlinks,
// malformed state, and unsupported versions rather than silently resetting.
func Open(identityDir string) (*Manager, error) {
	if strings.TrimSpace(identityDir) == "" {
		return nil, errors.New("agent access: identity directory is required")
	}
	if err := os.MkdirAll(identityDir, 0o700); err != nil {
		return nil, fmt.Errorf("agent access: create identity directory: %w", err)
	}
	dirInfo, err := os.Lstat(identityDir)
	if err != nil {
		return nil, fmt.Errorf("agent access: inspect identity directory: %w", err)
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return nil, errors.New("agent access: identity directory must be a real directory")
	}
	if err := os.Chmod(identityDir, 0o700); err != nil {
		return nil, fmt.Errorf("agent access: secure identity directory: %w", err)
	}

	m := &Manager{path: filepath.Join(identityDir, "agents.json"), agents: map[string]*storedAgent{}}
	info, err := os.Lstat(m.path)
	if errors.Is(err, os.ErrNotExist) {
		if err := m.persistLocked(); err != nil {
			return nil, err
		}
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agent access: inspect state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("agent access: state must be a regular file")
	}
	if err := os.Chmod(m.path, 0o600); err != nil {
		return nil, fmt.Errorf("agent access: secure state file: %w", err)
	}
	if info.Size() > maxStateBytes {
		return nil, errors.New("agent access: state file exceeds size limit")
	}
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return nil, fmt.Errorf("agent access: read state: %w", err)
	}
	var state diskState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("agent access: parse state: %w", err)
	}
	if state.Version != stateVersion {
		return nil, fmt.Errorf("agent access: unsupported state version %d", state.Version)
	}
	if len(state.Audit) > maxAuditEvents {
		return nil, errors.New("agent access: audit history exceeds size limit")
	}
	for i := range state.Agents {
		a := state.Agents[i]
		if err := validateStoredAgent(a); err != nil {
			return nil, fmt.Errorf("agent access: invalid stored identity %q: %w", a.ID, err)
		}
		if _, exists := m.agents[a.ID]; exists {
			return nil, fmt.Errorf("agent access: duplicate stored identity %q", a.ID)
		}
		a.Scopes = append([]string(nil), a.Scopes...)
		m.agents[a.ID] = &a
	}
	for _, event := range state.Audit {
		if event.At == "" || event.AgentID == "" || event.Action == "" || event.Actor != "local-admin" {
			return nil, errors.New("agent access: invalid audit event")
		}
	}
	m.audit = append([]Event(nil), state.Audit...)
	return m, nil
}

// reloadLocked observes atomically replaced state from another local MCP
// process, so rotation/disable/revoke applies to long-lived stdio sessions.
func (m *Manager) reloadLocked() error {
	info, err := os.Lstat(m.path)
	if err != nil {
		return fmt.Errorf("agent access: inspect state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("agent access: state must be a regular file")
	}
	if info.Size() > maxStateBytes {
		return errors.New("agent access: state file exceeds size limit")
	}
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return fmt.Errorf("agent access: read state: %w", err)
	}
	var state diskState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("agent access: parse state: %w", err)
	}
	if state.Version != stateVersion {
		return fmt.Errorf("agent access: unsupported state version %d", state.Version)
	}
	if len(state.Audit) > maxAuditEvents {
		return errors.New("agent access: audit history exceeds size limit")
	}
	agents := make(map[string]*storedAgent, len(state.Agents))
	for i := range state.Agents {
		a := state.Agents[i]
		if err := validateStoredAgent(a); err != nil {
			return fmt.Errorf("agent access: invalid stored identity %q: %w", a.ID, err)
		}
		if agents[a.ID] != nil {
			return fmt.Errorf("agent access: duplicate stored identity %q", a.ID)
		}
		a.Scopes = append([]string(nil), a.Scopes...)
		agents[a.ID] = &a
	}
	for _, event := range state.Audit {
		if event.At == "" || event.AgentID == "" || event.Action == "" || event.Actor != "local-admin" {
			return errors.New("agent access: invalid audit event")
		}
	}
	m.agents = agents
	m.audit = append([]Event(nil), state.Audit...)
	return nil
}

// Create registers an immutable agent ID/display name and returns its new
// bearer token exactly once. Only the SHA-256 verifier is persisted.
func (m *Manager) Create(displayName string, scopes []string) (Agent, string, error) {
	if m == nil {
		return Agent{}, "", errors.New("agent access: unavailable")
	}
	name, err := validateDisplayName(displayName)
	if err != nil {
		return Agent{}, "", err
	}
	normalizedScopes, err := validateScopes(scopes)
	if err != nil {
		return Agent{}, "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reloadLocked(); err != nil {
		return Agent{}, "", err
	}
	for _, existing := range m.agents {
		if strings.EqualFold(existing.DisplayName, name) {
			return Agent{}, "", errors.New("agent access: display name already exists")
		}
	}
	id, err := randomID()
	if err != nil {
		return Agent{}, "", err
	}
	for m.agents[id] != nil {
		id, err = randomID()
		if err != nil {
			return Agent{}, "", err
		}
	}
	token, err := newToken(id)
	if err != nil {
		return Agent{}, "", err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	a := &storedAgent{
		ID: id, DisplayName: name, Scopes: normalizedScopes,
		CreatedAt: now, UpdatedAt: now, TokenHash: tokenHash(token),
	}
	beforeAudit := append([]Event(nil), m.audit...)
	m.agents[id] = a
	m.addEvent(id, "created", now)
	if err := m.persistLocked(); err != nil {
		delete(m.agents, id)
		m.audit = beforeAudit
		return Agent{}, "", err
	}
	return publicAgent(a), token, nil
}

// Rotate replaces the credential for an existing identity without changing
// its ID, display name, scopes, or disabled state.
func (m *Manager) Rotate(id string) (Agent, string, error) {
	if m == nil {
		return Agent{}, "", errors.New("agent access: unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reloadLocked(); err != nil {
		return Agent{}, "", err
	}
	a := m.agents[id]
	if a == nil {
		return Agent{}, "", errors.New("agent access: identity not found")
	}
	if a.Revoked {
		return Agent{}, "", errors.New("agent access: revoked identity cannot be rotated")
	}
	token, err := newToken(id)
	if err != nil {
		return Agent{}, "", err
	}
	before := *a
	before.Scopes = append([]string(nil), a.Scopes...)
	beforeAudit := append([]Event(nil), m.audit...)
	a.TokenHash = tokenHash(token)
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.addEvent(id, "rotated", a.UpdatedAt)
	if err := m.persistLocked(); err != nil {
		*a = before
		m.audit = beforeAudit
		return Agent{}, "", err
	}
	return publicAgent(a), token, nil
}

// Disable immediately makes the current credential unusable while preserving
// the identity and verifier so an administrator can explicitly re-enable it.
func (m *Manager) Disable(id string) (Agent, error) {
	return m.setDisabled(id, true)
}

// Enable reactivates a disabled, non-revoked credential.
func (m *Manager) Enable(id string) (Agent, error) {
	return m.setDisabled(id, false)
}

func (m *Manager) setDisabled(id string, disabled bool) (Agent, error) {
	if m == nil {
		return Agent{}, errors.New("agent access: unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reloadLocked(); err != nil {
		return Agent{}, err
	}
	a := m.agents[id]
	if a == nil {
		return Agent{}, errors.New("agent access: identity not found")
	}
	if a.Revoked {
		return Agent{}, errors.New("agent access: revoked identity cannot be re-enabled")
	}
	if a.Disabled == disabled {
		return publicAgent(a), nil
	}
	before := *a
	before.Scopes = append([]string(nil), a.Scopes...)
	beforeAudit := append([]Event(nil), m.audit...)
	a.Disabled = disabled
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	action := "enabled"
	if disabled {
		action = "disabled"
	}
	m.addEvent(id, action, a.UpdatedAt)
	if err := m.persistLocked(); err != nil {
		*a = before
		m.audit = beforeAudit
		return Agent{}, err
	}
	return publicAgent(a), nil
}

// Revoke permanently invalidates a credential but retains the immutable
// identity record and administrative history.
func (m *Manager) Revoke(id string) (Agent, error) {
	if m == nil {
		return Agent{}, errors.New("agent access: unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reloadLocked(); err != nil {
		return Agent{}, err
	}
	a := m.agents[id]
	if a == nil {
		return Agent{}, errors.New("agent access: identity not found")
	}
	if a.Revoked {
		return publicAgent(a), nil
	}
	before := *a
	before.Scopes = append([]string(nil), a.Scopes...)
	beforeAudit := append([]Event(nil), m.audit...)
	a.Disabled = true
	a.Revoked = true
	a.TokenHash = ""
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.addEvent(id, "revoked", a.UpdatedAt)
	if err := m.persistLocked(); err != nil {
		*a = before
		m.audit = beforeAudit
		return Agent{}, err
	}
	return publicAgent(a), nil
}

// SetScopes replaces the exact MCP tool grants for an identity. Unknown tool
// names are rejected; an empty list intentionally grants no tool execution.
func (m *Manager) SetScopes(id string, scopes []string) (Agent, error) {
	if m == nil {
		return Agent{}, errors.New("agent access: unavailable")
	}
	normalized, err := validateScopes(scopes)
	if err != nil {
		return Agent{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reloadLocked(); err != nil {
		return Agent{}, err
	}
	a := m.agents[id]
	if a == nil {
		return Agent{}, errors.New("agent access: identity not found")
	}
	if a.Revoked {
		return Agent{}, errors.New("agent access: revoked identity cannot change scopes")
	}
	if equalStrings(a.Scopes, normalized) {
		return publicAgent(a), nil
	}
	before := *a
	before.Scopes = append([]string(nil), a.Scopes...)
	beforeAudit := append([]Event(nil), m.audit...)
	a.Scopes = normalized
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.addEvent(id, "scopes_changed", a.UpdatedAt)
	if err := m.persistLocked(); err != nil {
		*a = before
		m.audit = beforeAudit
		return Agent{}, err
	}
	return publicAgent(a), nil
}

// List returns copies of every identity and the newest administrative events.
// No token or verifier is included in the returned agents.
func (m *Manager) List() ([]Agent, []Event) {
	if m == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reloadLocked(); err != nil {
		return nil, nil
	}
	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	agents := make([]Agent, 0, len(ids))
	for _, id := range ids {
		agents = append(agents, publicAgent(m.agents[id]))
	}
	events := append([]Event(nil), m.audit...)
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return agents, events
}

// Authenticate validates a bearer token and returns its server-bound actor,
// display name, and exact tool scopes. Caller-supplied names are never read.
func (m *Manager) Authenticate(token string) (actor, displayName string, scopes []string, ok bool) {
	if m == nil || len(token) > 256 {
		return "", "", nil, false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "lo1" || !validID(parts[1]) {
		return "", "", nil, false
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(secret) != 32 {
		return "", "", nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reloadLocked(); err != nil {
		return "", "", nil, false
	}
	a := m.agents[parts[1]]
	if a == nil || a.Disabled || a.Revoked || a.TokenHash == "" {
		return "", "", nil, false
	}
	want, err := hex.DecodeString(a.TokenHash)
	if err != nil {
		return "", "", nil, false
	}
	got := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(got[:], want) != 1 {
		return "", "", nil, false
	}
	return "agent:" + a.ID, a.DisplayName, append([]string(nil), a.Scopes...), true
}

func (m *Manager) addEvent(id, action, at string) {
	m.audit = append(m.audit, Event{At: at, AgentID: id, Action: action, Actor: "local-admin"})
	if len(m.audit) > maxAuditEvents {
		m.audit = append([]Event(nil), m.audit[len(m.audit)-maxAuditEvents:]...)
	}
}

func (m *Manager) persistLocked() error {
	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	state := diskState{Version: stateVersion, Agents: make([]storedAgent, 0, len(ids)), Audit: append([]Event(nil), m.audit...)}
	for _, id := range ids {
		a := *m.agents[id]
		a.Scopes = append([]string(nil), a.Scopes...)
		state.Agents = append(state.Agents, a)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("agent access: encode state: %w", err)
	}
	if len(raw) > maxStateBytes {
		return errors.New("agent access: state file exceeds size limit")
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".agents-*.tmp")
	if err != nil {
		return fmt.Errorf("agent access: create state temp file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("agent access: secure state temp file: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return fmt.Errorf("agent access: write state: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("agent access: sync state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("agent access: close state: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return fmt.Errorf("agent access: commit state: %w", err)
	}
	if dir, err := os.Open(filepath.Dir(m.path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func publicAgent(a *storedAgent) Agent {
	return Agent{
		ID: a.ID, DisplayName: a.DisplayName, Scopes: append([]string(nil), a.Scopes...),
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, Disabled: a.Disabled, Revoked: a.Revoked,
	}
}

func validateStoredAgent(a storedAgent) error {
	if !validID(a.ID) {
		return errors.New("invalid agent ID")
	}
	if _, err := validateDisplayName(a.DisplayName); err != nil {
		return err
	}
	if _, err := validateScopes(a.Scopes); err != nil {
		return err
	}
	if a.CreatedAt == "" || a.UpdatedAt == "" {
		return errors.New("missing identity timestamps")
	}
	if a.Revoked {
		if !a.Disabled || a.TokenHash != "" {
			return errors.New("revoked identity still has active credential state")
		}
		return nil
	}
	hash, err := hex.DecodeString(a.TokenHash)
	if err != nil || len(hash) != sha256.Size {
		return errors.New("invalid credential verifier")
	}
	return nil
}

func validateDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 {
		return "", errors.New("agent access: display name must be 1–80 valid Unicode characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("agent access: display name cannot contain control characters")
		}
	}
	return name, nil
}

func validateScopes(scopes []string) ([]string, error) {
	allowed := make(map[string]bool)
	for _, name := range mcp.ToolNames() {
		allowed[name] = true
	}
	seen := make(map[string]bool, len(scopes))
	normalized := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if !allowed[scope] {
			return nil, fmt.Errorf("agent access: %q is not an MCP tool scope", scope)
		}
		if !seen[scope] {
			seen[scope] = true
			normalized = append(normalized, scope)
		}
	}
	sort.Strings(normalized)
	return normalized, nil
}

func validID(id string) bool {
	if len(id) != 36 || !strings.HasPrefix(id, "agt_") {
		return false
	}
	_, err := hex.DecodeString(id[4:])
	return err == nil
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("agent access: generate identity ID: %w", err)
	}
	return "agt_" + hex.EncodeToString(raw[:]), nil
}

func newToken(id string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("agent access: generate credential: %w", err)
	}
	return "lo1." + id + "." + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
