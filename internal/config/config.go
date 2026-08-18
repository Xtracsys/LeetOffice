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

	// Path is where this config was loaded from (not serialized); it lets the
	// service installer and MCP snippets reference the real file location.
	Path string `json:"-"`
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
