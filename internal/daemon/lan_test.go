package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leetoffice/internal/config"
)

func TestStartAtPathFailsClosedForLANBeforeOpeningStore(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	cfg := config.Default(storeDir, "human:test")
	cfg.Listen.HTTP = "192.168.1.20:7667"
	cfg.LAN.Enabled = true
	cfg.LAN.Hostname = "leetoffice.local"
	cfgPath := filepath.Join(dir, "node.json")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}

	_, _, err := StartAtPath(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "LAN mode is not available") {
		t.Fatalf("StartAtPath error = %v, want fail-closed LAN mode error", err)
	}
	if _, statErr := os.Stat(storeDir); !os.IsNotExist(statErr) {
		t.Fatalf("store was opened before LAN security gate: stat error = %v", statErr)
	}
}
