// Package daemon is the always-on node process (leetd): local store + git,
// the localhost human UI, the MCP surface, short-cadence sync (D5), and the
// coordinator services (git-over-mTLS server, enrollment, discovery) when the
// node is promoted. Headless — no browser dependency (D14).
package daemon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"leetoffice/internal/config"
	"leetoffice/internal/httpui"
	"leetoffice/internal/mcp"
	"leetoffice/internal/memory"
	leetNet "leetoffice/internal/net"
	"leetoffice/internal/rag"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

// Node is one running LeetOffice node.
type Node struct {
	Cfg     *config.Config
	Store   *store.Store
	Repo    *leetSync.Repo
	MCP     *mcp.Server
	cfgPath string
	enroll  *leetNet.EnrollmentServer // live coordinator secret; rotated from settings
	// syncEveryCh tells syncLoop to Reset the ticker from Cfg.SyncEverySec.
	syncEveryCh chan struct{}
	syncHook    func(reason string) // tests: fires at the start of each syncOnce
}

// Start opens the store and repo described by cfg.
func Start(cfg *config.Config) (*Node, error) {
	s, err := store.OpenStore(cfg.StoreDir)
	if err != nil {
		return nil, err
	}
	repo, err := leetSync.Init(cfg.StoreDir)
	if err != nil {
		return nil, err
	}
	if cfg.MainShare != "" {
		if err := repo.AddRemote("origin", cfg.MainShare); err != nil {
			return nil, err
		}
	}
	// One-shot CLI (leetd sync, …) never reaches StartLoops/startClient,
	// so a leet:// remote used to fail with unsupported scheme "leet".
	if strings.HasPrefix(cfg.MainShare, leetNet.Scheme+"://") {
		if _, err := installLeetTransport(cfg); err != nil {
			return nil, err
		}
	}
	actor := cfg.Actor
	mcpSrv := mcp.NewServer(s, repo, searchBackend(cfg, s), actor)
	return &Node{Cfg: cfg, Store: s, Repo: repo, MCP: mcpSrv, syncEveryCh: make(chan struct{}, 1)}, nil
}

// StartAtPath opens the node described by a config file path.
func StartAtPath(cfgPath string) (*Node, *config.Config, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, err
	}
	n, err := Start(cfg)
	if err != nil {
		return nil, cfg, err
	}
	n.cfgPath = cfgPath
	return n, cfg, nil
}

// CfgFilePath reports where this node's config lives ("" if in-memory).
func (n *Node) CfgFilePath() string { return n.cfgPath }

// searchBackend picks semantic search when Ollama is up, else the always-on
// keyword fallback (D17; RUNBOOK allows the stub as long as search returns).
func searchBackend(cfg *config.Config, s *store.Store) mcp.SearchFunc {
	ollama := &rag.Ollama{BaseURL: cfg.Ollama.BaseURL, Model: cfg.Ollama.Model}
	return func(query, typ string, tags []string, limit int) ([]mcp.Hit, error) {
		var hits []rag.Hit
		var err error
		if ollama.Available() {
			hits, err = ollama.SearchSemantic(s, query, typ, tags, limit)
		} else {
			hits, err = rag.Search(s, query, typ, tags, limit)
		}
		if err != nil {
			return nil, err
		}
		out := make([]mcp.Hit, 0, len(hits))
		for _, h := range hits {
			out = append(out, mcp.Hit(h))
		}
		return out, nil
	}
}

// ServeHTTP mounts the human UI and the MCP HTTP surface on one handler.
func (n *Node) ServeHTTP() http.Handler {
	mux := http.NewServeMux()
	bin, _ := os.Executable()
	ui := &httpui.UI{Store: n.Store, Repo: n.Repo, Config: n.Cfg, BinaryPath: bin, CfgPath: n.cfgPath}
	if n.enroll != nil {
		ui.RotateEnrollment = n.enroll.SetSecret
	}
	ui.RescheduleSync = n.RescheduleSync
	mux.Handle("/", ui.Handler())
	mux.Handle("/mcp", n.MCP.Handler())
	mux.HandleFunc("/service/install", handleServiceInstall(n.Cfg))
	mux.HandleFunc("/service/uninstall", handleServiceUninstall())
	return mux
}

// StartLoops runs the background work a node owns: coordinator networking
// (mTLS git service, enrollment, mDNS), short-cadence sync (D5), and the
// automation jobs (M19). HTTP serving is the caller's concern — see
// ListenAndServe, which can boot a node after first-run setup completes
// without restarting the process.
func (n *Node) StartLoops(ctx context.Context) error {
	if n.Cfg.IsCoordinator() || strings.HasPrefix(n.Cfg.MainShare, "leet://") {
		if err := n.StartNetworking(ctx); err != nil {
			return err
		}
	}
	if n.Cfg.MainShare != "" {
		go n.syncLoop(ctx)
	}
	go n.jobsLoop(ctx)
	return nil
}

func (n *Node) syncEvery() time.Duration {
	sec := n.Cfg.SyncEverySec
	if sec <= 0 {
		sec = config.DefaultSyncEvery
	}
	return time.Duration(sec) * time.Second
}

// RescheduleSync restarts the running sync ticker from Cfg.SyncEverySec
// so a settings save takes effect without restarting leetd.
func (n *Node) RescheduleSync() {
	if n.syncEveryCh == nil {
		return
	}
	select {
	case n.syncEveryCh <- struct{}{}:
	default:
	}
}

func (n *Node) syncLoop(ctx context.Context) {
	t := time.NewTicker(n.syncEvery())
	defer t.Stop()
	n.syncOnce("timer")
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.syncEveryCh:
			t.Reset(n.syncEvery())
		case <-t.C:
			n.syncOnce("timer")
		}
	}
}

// SyncOnce performs one fetch→merge→push cycle (§6.5) and reports conflicts.
func (n *Node) SyncOnce() (*leetSync.SyncResult, error) {
	if n.Cfg.MainShare == "" {
		return nil, fmt.Errorf("no main share configured")
	}
	res, err := n.Repo.Sync("origin", n.Cfg.Actor)
	if err != nil {
		return res, err
	}
	for _, c := range res.Conflicts {
		log.Printf("conflict: %s block %s — both versions retained", c.Slug, c.BlockID)
	}
	return res, nil
}

func (n *Node) syncOnce(reason string) {
	if n.syncHook != nil {
		n.syncHook(reason)
	}
	res, err := n.SyncOnce()
	if err != nil {
		log.Printf("sync (%s): %v", reason, err)
		return
	}
	if res.Pulled || res.Pushed || res.Merged {
		log.Printf("sync (%s): pulled=%v pushed=%v merged=%v conflicts=%d",
			reason, res.Pulled, res.Pushed, res.Merged, len(res.Conflicts))
	}
}

// jobsLoop runs the automation suite (M19): debounced memory synthesis on
// store change (D9), the daily digest (D16), and hourly doc hygiene + monitor
// notices (§7.3). Everything is best-effort: a failure logs and waits for the
// next tick.
func (n *Node) jobsLoop(ctx context.Context) {
	lastSynth := n.storeFingerprint() // no commit when nothing changed since start
	// Do not seed lastDigestDay to today: that skipped the first write until
	// the next UTC midnight (D16). Write today's digest on start; later
	// ticks only fire when the calendar day changes.
	lastDigestDay := n.writeDailyDigest()
	synthT := time.NewTicker(60 * time.Second)
	digestT := time.NewTicker(10 * time.Minute)
	hygT := time.NewTicker(time.Hour)
	defer synthT.Stop()
	defer digestT.Stop()
	defer hygT.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-synthT.C:
			fp := n.storeFingerprint()
			if fp == lastSynth {
				break
			}
			if err := memory.Synthesize(n.Store, n.Repo, n.Cfg.Actor); err != nil {
				log.Printf("memory: %v", err)
				break
			}
			lastSynth = fp
		case <-digestT.C:
			day := time.Now().UTC().Format("2006-01-02")
			if day == lastDigestDay {
				break
			}
			if wrote := n.writeDailyDigest(); wrote != "" {
				lastDigestDay = wrote
			}
		case <-hygT.C:
			issues, err := memory.Hygiene(n.Store, 0)
			if err != nil {
				log.Printf("hygiene: %v", err)
				break
			}
			if err := n.writeNotice(issues); err != nil {
				log.Printf("notice: %v", err)
			}
			for _, i := range issues {
				log.Printf("hygiene: %s: %s", i.Kind, i.Detail)
			}
		}
	}
}

// writeDailyDigest writes today's UTC digest (D16). Returns the day string
// on success, or "" so the next tick retries.
func (n *Node) writeDailyDigest() string {
	day := time.Now().UTC()
	if _, err := memory.DailyDigest(n.Store, n.Repo, day, n.Cfg.Actor); err != nil {
		log.Printf("digest: %v", err)
		return ""
	}
	return day.Format("2006-01-02")
}

// storeFingerprint hashes the documents themselves, NOT git HEAD: synthesis
// changes HEAD (it commits MEMORY.md), so keying on HEAD made the job
// re-trigger on its own output every tick. Keying on doc state means we only
// re-synthesize when the underlying store actually changed.
func (n *Node) storeFingerprint() string {
	docs, err := n.Store.List()
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, d := range docs {
		fmt.Fprintf(&b, "%s:%d:%s;", d.ID, d.Version, d.Updated)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
}

// writeNotice maintains NOTICE.md in the store — the monitor's human-facing
// alert surface (§7.3).
func (n *Node) writeNotice(issues []memory.Issue) error {
	var b strings.Builder
	b.WriteString("# Notices\n\n_Generated by the consistency monitor. Empty list = all clear._\n\n")
	if len(issues) == 0 {
		b.WriteString("- none\n")
	} else {
		for _, i := range issues {
			fmt.Fprintf(&b, "- **%s**: %s\n", i.Kind, i.Detail)
		}
	}
	return os.WriteFile(filepath.Join(n.Store.Root, "NOTICE.md"), []byte(b.String()), 0o644)
}

// InitBareShare creates the main share bare repo for a coordinator config at
// the path given by a file:// MainShare URL.
func InitBareShare(cfg *config.Config) (string, error) {
	raw := cfg.MainShare
	raw = trimScheme(raw)
	if raw == "" {
		return "", fmt.Errorf("coordinator needs a main share URL (file:///path/main.git)")
	}
	if _, err := leetSync.InitBare(raw); err != nil {
		return "", err
	}
	return raw, nil
}

func trimScheme(url string) string {
	for _, p := range []string{"file://", "leet://", "ssh://", "http://", "https://"} {
		if len(url) >= len(p) && url[:len(p)] == p {
			if p == "file://" {
				return url[len(p):]
			}
			return "" // non-local scheme — no local path to create
		}
	}
	return url
}
