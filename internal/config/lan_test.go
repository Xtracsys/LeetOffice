package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRejectsNonLoopbackHTTPWithoutLANOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.json")
	data := []byte(`{"node_id":"test-node","actor":"human:test","store_dir":"/tmp/leetoffice-test","listen":{"http":"0.0.0.0:7667"}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected non-loopback listener without explicit LAN opt-in to be rejected")
	}
}

func TestDefaultHTTPListenIsLoopback(t *testing.T) {
	cfg := Default(t.TempDir(), "human:test")
	if cfg.Listen.HTTP != "127.0.0.1:7667" {
		t.Fatalf("default HTTP listener = %q, want loopback", cfg.Listen.HTTP)
	}
}

func TestLoadRejectsWildcardLANBindEvenWhenOptedIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.json")
	data := []byte(`{"node_id":"test-node","actor":"human:test","store_dir":"/tmp/leetoffice-test","listen":{"http":":7667"},"lan":{"enabled":true,"hostname":"office.local"}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected wildcard LAN bind to be rejected")
	}
}

func TestLoadRejectsPublicLANBind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.json")
	data := []byte(`{"node_id":"test-node","actor":"human:test","store_dir":"/tmp/leetoffice-test","listen":{"http":"8.8.8.8:7667"},"lan":{"enabled":true,"hostname":"office.example"}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected non-private LAN bind to be rejected")
	}
}
