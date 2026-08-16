// Package config holds the LeetOffice node configuration (M22): a single
// JSON file describing identity, role, store location, and endpoints.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// Default builds a sensible fat-client config for a store dir.
func Default(storeDir, actor string) *Config {
	c := &Config{
		NodeID:       hostOrDefault(),
		Actor:        actor,
		Role:         "client",
		StoreDir:     storeDir,
		MainShare:    "",
		SyncEverySec: DefaultSyncEvery,
		IdentityDir:  filepath.Join(storeDir, "..", "identity"),
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
		c.IdentityDir = filepath.Join(c.StoreDir, "..", "identity")
	}
	if c.Actor == "" {
		c.Actor = "human:" + c.NodeID
	}
}

// IsCoordinator reports whether this node runs the coordinator services.
func (c *Config) IsCoordinator() bool { return c.Role == "coordinator" }
