package daemon

import (
	"context"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"leetoffice/internal/config"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

// TestSyncEverySecRestartsTicker is the settings-cadence gate: saving
// sync_every_sec used to write node.json while syncLoop kept the ticker
// created at start. A 10-minute interval must become 1s without restart.
func TestSyncEverySecRestartsTicker(t *testing.T) {
	dir := t.TempDir()
	bare := filepath.Join(dir, "main.git")
	if _, err := leetSync.InitBare(bare); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, "store")
	cfgPath := filepath.Join(dir, "node.json")
	cfg := config.Default(storeDir, "human:josh")
	cfg.MainShare = "file://" + bare
	cfg.SyncEverySec = 600
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}

	n, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	n.cfgPath = cfgPath
	var ticks atomic.Int64
	n.syncHook = func(string) { ticks.Add(1) }

	d := store.NewDoc(store.TypeDoc, "spec", "Spec")
	d.AddParagraph("seed")
	if err := n.Store.Save(d, "human:josh"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Repo.CommitAll("human:josh", "seed"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.syncLoop(ctx)

	waitTicks(t, &ticks, 1, 2*time.Second)
	if got := ticks.Load(); got != 1 {
		t.Fatalf("startup should fire once, got %d", got)
	}

	h := httptest.NewServer(n.ServeHTTP())
	defer h.Close()
	res, err := h.Client().PostForm(h.URL+"/settings", url.Values{
		"actor": {"josh"}, "sync_every_sec": {"1"},
		"ollama_base": {n.Cfg.Ollama.BaseURL}, "ollama_model": {n.Cfg.Ollama.Model}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if n.Cfg.SyncEverySec != 1 {
		t.Fatalf("config cadence = %d", n.Cfg.SyncEverySec)
	}

	waitTicks(t, &ticks, 3, 3*time.Second)
}

func waitTicks(t *testing.T, ticks *atomic.Int64, want int64, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for ticks.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("sync ticks = %d, want >= %d within %s", ticks.Load(), want, d)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
