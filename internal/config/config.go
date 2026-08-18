// Package config holds the LeetOffice node configuration (M22): a single
// JSON file describing identity, role, store location, and endpoints.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Defaults for a fat-client node. The main share is configurable (D10).
const (
	DefaultHTTPListen = "127.0.0.1:7667"
	DefaultGitListen  = ":7666"
	DefaultSyncEvery  = 5 // seconds (D5 short-cadence)
)

// Config is the node configuration.
type Config struct {
	NodeID    string `json:"node_id"`
	Actor     string `json:"actor"` // "human:<id>" or "agent:<id>" (D7)
	Role      string `json:"role"`  // "client" | "coordinator"
	StoreDir  string `json:"store_dir"`
	MainShare string `json:"main_share"` // file://…/main.git or leet://host:port/main.git

	Listen struct {
		HTTP   string `json:"http"`   // editor UI + MCP over HTTP (localhost)
		Git    string `json:"git"`    // mTLS git transport (coordinator)
		Enroll string `json:"enroll"` // enrollment endpoint (coordinator, D8)
	} `json:"listen"`

	EnrollmentSecret string `json:"enrollment_secret,omitempty"` // coordinator only (D8)
	SyncEverySec     int    `json:"sync_every_sec"`

	Ollama struct {
		BaseURL string `json:"base_url"`
		Model   string `json:"model"`
	} `json:"ollama"`

	IdentityDir string `json:"identity_dir"` // certs/keys (mTLS)

	// HiddenActors are names dropped from chat presence and the Settings
	// "recently active" list. Git History is unchanged (D3). Local to this
	// node — not team membership (that is the issued certificate).
	HiddenActors []string `json:"hidden_actors,omitempty"`

	// AgentSubscriptions is the durable agent inbox watch list (actor →
	// channels + per-channel read cursor). Local to this node.
	AgentSubscriptions []AgentSubscription `json:"agent_subscriptions,omitempty"`

	// Path is where this config was loaded from (not serialized); it lets the
	// service installer and MCP snippets reference the real file location.
	Path string `json:"-"`
}

// AgentSubscription is one --actor's channel watch and read cursors.
type AgentSubscription struct {
	Actor    string            `json:"actor"`
	Channels []string          `json:"channels"`         // empty = all channels
	Cursor   map[string]string `json:"cursor,omitempty"` // slug → last-seen RFC3339 ts
}

// IsHidden reports whether name is on the local hide list.
func (c *Config) IsHidden(name string) bool {
	if c == nil {
		return false
	}
	name = strings.TrimSpace(name)
	for _, h := range c.HiddenActors {
		if h == name {
			return true
		}
	}
	return false
}

// HideActor adds name to the local hide list. You cannot hide yourself.
func (c *Config) HideActor(name string) {
	if c == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" || name == c.Actor || name == c.NodeID || c.IsHidden(name) {
		return
	}
	c.HiddenActors = append(c.HiddenActors, name)
}

// CanonicalActor turns "hermes" into "agent:hermes". Prefixed ids are kept.
func CanonicalActor(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if !strings.Contains(s, ":") {
		return "agent:" + s
	}
	return s
}

// ActorKey is a case-folded comparison key for agent ids.
func ActorKey(s string) string {
	s = CanonicalActor(s)
	if i := strings.Index(s, ":"); i >= 0 {
		return strings.ToLower(s[:i]) + ":" + strings.ToLower(s[i+1:])
	}
	return strings.ToLower(s)
}

// SameActor reports whether a and b name the same actor.
func SameActor(a, b string) bool { return ActorKey(a) == ActorKey(b) }

// Subscribe registers actor for channels (empty = all). Replaces an existing
// watch for the same actor and keeps its cursors.
func (c *Config) Subscribe(actor string, channels []string) AgentSubscription {
	actor = CanonicalActor(actor)
	var keep map[string]string
	out := c.AgentSubscriptions[:0]
	if c.AgentSubscriptions == nil {
		out = nil
	}
	for _, sub := range c.AgentSubscriptions {
		if SameActor(sub.Actor, actor) {
			keep = sub.Cursor
			continue
		}
		out = append(out, sub)
	}
	norm := make([]string, 0, len(channels))
	seen := map[string]bool{}
	for _, ch := range channels {
		ch = strings.TrimSpace(strings.TrimPrefix(ch, "#"))
		if ch == "" || seen[ch] {
			continue
		}
		seen[ch] = true
		norm = append(norm, ch)
	}
	sub := AgentSubscription{Actor: actor, Channels: norm, Cursor: keep}
	c.AgentSubscriptions = append(out, sub)
	return sub
}

// Subscription finds the watch for actor.
func (c *Config) Subscription(actor string) (AgentSubscription, bool) {
	if c == nil {
		return AgentSubscription{}, false
	}
	for _, sub := range c.AgentSubscriptions {
		if SameActor(sub.Actor, actor) {
			return sub, true
		}
	}
	return AgentSubscription{}, false
}

// Watches reports whether actor is subscribed to slug (or to all channels).
func (c *Config) Watches(actor, slug string) bool {
	sub, ok := c.Subscription(actor)
	if !ok {
		return false
	}
	if len(sub.Channels) == 0 {
		return true
	}
	slug = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(slug), "#"))
	for _, ch := range sub.Channels {
		if strings.EqualFold(ch, slug) {
			return true
		}
	}
	return false
}

// MarkRead records actor's cursor for channel (RFC3339 ts).
func (c *Config) MarkRead(actor, channel, ts string) bool {
	actor = CanonicalActor(actor)
	channel = strings.TrimSpace(strings.TrimPrefix(channel, "#"))
	ts = strings.TrimSpace(ts)
	if actor == "" || channel == "" || ts == "" {
		return false
	}
	for i := range c.AgentSubscriptions {
		if !SameActor(c.AgentSubscriptions[i].Actor, actor) {
			continue
		}
		if c.AgentSubscriptions[i].Cursor == nil {
			c.AgentSubscriptions[i].Cursor = map[string]string{}
		}
		c.AgentSubscriptions[i].Cursor[channel] = ts
		return true
	}
	return false
}

// UnhideActor removes name from the hide list.
func (c *Config) UnhideActor(name string) {
	if c == nil || len(c.HiddenActors) == 0 {
		return
	}
	name = strings.TrimSpace(name)
	out := c.HiddenActors[:0]
	for _, h := range c.HiddenActors {
		if h != name {
			out = append(out, h)
		}
	}
	c.HiddenActors = out
}

// DefaultPath is the per-user config location.
func DefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if runtime.GOOS == "windows" {
			base = filepath.Join(os.Getenv("APPDATA"), "leetoffice")
		} else {
			base = filepath.Join(home(), ".config", "leetoffice")
		}
	}
	return filepath.Join(base, "node.json")
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// DefaultStoreDir is the product store location (~/LeetOffice). The
// first-run wizard and `leetd init` both use this so mixing them finds
// the same files.
func DefaultStoreDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		wd, _ := os.Getwd()
		return filepath.Join(wd, "LeetOffice")
	}
	return filepath.Join(h, "LeetOffice")
}

// IdentityDirFor is the identity directory for a store: a sibling
// .leetoffice-identity outside the git worktree. Wizard and leetd init
// must agree — mixing them used to look in identity vs .leetoffice-identity.
func IdentityDirFor(storeDir string) string {
	if storeDir == "" {
		storeDir = DefaultStoreDir()
	}
	return filepath.Join(filepath.Dir(storeDir), ".leetoffice-identity")
}

// Default builds a sensible fat-client config for a store dir.
func Default(storeDir, actor string) *Config {
	if storeDir == "" {
		storeDir = DefaultStoreDir()
	}
	c := &Config{
		NodeID:       hostOrDefault(),
		Actor:        actor,
		Role:         "client",
		StoreDir:     storeDir,
		MainShare:    "",
		SyncEverySec: DefaultSyncEvery,
		IdentityDir:  IdentityDirFor(storeDir),
	}
	c.Listen.HTTP = DefaultHTTPListen
	c.Listen.Git = DefaultGitListen
	c.Ollama.BaseURL = "http://127.0.0.1:11434"
	c.Ollama.Model = "nomic-embed-text"
	return c
}

func hostOrDefault() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "node"
	}
	return h
}

// Load reads a config file; missing file returns os.ErrNotExist unwrapped.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c.fillDefaults()
	c.Path = path
	return &c, nil
}

// Save writes the config atomically enough for a local node.
func (c *Config) Save(path string) error {
	c.fillDefaults()
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func (c *Config) fillDefaults() {
	if c.SyncEverySec <= 0 {
		c.SyncEverySec = DefaultSyncEvery
	}
	if c.Listen.HTTP == "" {
		c.Listen.HTTP = DefaultHTTPListen
	}
	if c.Listen.Git == "" {
		c.Listen.Git = DefaultGitListen
	}
	if c.Listen.Enroll == "" {
		c.Listen.Enroll = ":7443"
	}
	if c.Ollama.BaseURL == "" {
		c.Ollama.BaseURL = "http://127.0.0.1:11434"
	}
	if c.Ollama.Model == "" {
		c.Ollama.Model = "nomic-embed-text"
	}
	if c.IdentityDir == "" {
		c.IdentityDir = IdentityDirFor(c.StoreDir)
	}
	if c.Actor == "" {
		c.Actor = "human:" + c.NodeID
	}
}

// IsCoordinator reports whether this node runs the coordinator services.
func (c *Config) IsCoordinator() bool { return c.Role == "coordinator" }
