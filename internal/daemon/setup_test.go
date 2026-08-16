package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leetoffice/internal/config"
	leetNet "leetoffice/internal/net"
)

func TestCreateTeamConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "node.json")
	storeDir := filepath.Join(dir, "store")

	secret, err := CreateTeam(cfgPath, storeDir, "human:josh")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if len(secret) < 8 {
		t.Fatalf("secret too short: %q", secret)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load created config: %v", err)
	}
	if cfg.Role != "coordinator" || !strings.HasPrefix(cfg.MainShare, "file://") {
		t.Fatalf("config = role %s share %s", cfg.Role, cfg.MainShare)
	}
	if cfg.EnrollmentSecret != secret {
		t.Fatal("secret not persisted in config")
	}
	if _, err := os.Stat(filepath.Join(dir, "main-share.git")); err != nil {
		t.Fatalf("bare share not created: %v", err)
	}
	if strings.HasPrefix(cfg.IdentityDir, storeDir) {
		t.Fatal("identity dir must live outside the store (it is a git repo)")
	}
}

func TestJoinTeamEnrollsAndConfigures(t *testing.T) {
	dir := t.TempDir()
	ca, err := leetNet.CreateCA(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatal(err)
	}
	coord, err := ca.Issue("coordinator")
	if err != nil {
		t.Fatal(err)
	}
	// the git service runs on its OWN port (here ephemeral, unlike enroll) —
	// exactly the production topology that once made joined nodes sync
	// against the enrollment port and end up isolated
	gitRoot := filepath.Join(dir, "shares")
	if err := os.MkdirAll(gitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	gitSrv, err := leetNet.ServeGit("127.0.0.1:0", coord.ServerTLSConfig(), gitRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer gitSrv.Close()
	enr, err := leetNet.NewEnrollmentServer(ca, "one-time-secret", "127.0.0.1:0",
		coord.EnrollmentTLSConfig(), gitSrv.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatal(err)
	}
	defer enr.Close()

	cfgPath := filepath.Join(dir, "node.json")
	storeDir := filepath.Join(dir, "client-store")
	if err := JoinTeam(cfgPath, storeDir, "human:maya", enr.Addr().String(), "one-time-secret"); err != nil {
		t.Fatalf("JoinTeam: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	// REGRESSION (two-port bug): the share must point at the GIT service,
	// never the enrollment port we dialed to join.
	wantShare := "leet://" + gitSrv.Addr().String() + "/main.git"
	if cfg.MainShare != wantShare {
		t.Fatalf("share = %s, want %s (git port %v, not enrollment port %v)",
			cfg.MainShare, wantShare, gitSrv.Addr(), enr.Addr())
	}
	if _, err := leetNet.LoadIdentity(cfg.IdentityDir); err != nil {
		t.Fatalf("identity not saved: %v", err)
	}

	// wrong secret is rejected with a clear error
	bad := filepath.Join(dir, "bad.json")
	if err := JoinTeam(bad, filepath.Join(dir, "bad-store"), "human:eve",
		enr.Addr().String(), "wrong"); err == nil {
		t.Fatal("wrong secret accepted")
	}
}

// TestSetupWizardFlow drives the wizard handler exactly as the browser does:
// POST an action, expect the node to come alive without a process restart.
func TestSetupWizardFlow(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "node.json")
	d := &Daemon{cfgPath: cfgPath, ctx: context.Background()}
	d.mu.Lock()
	d.handler = d.setupHandler()
	d.mu.Unlock()
	h := httptest.NewServer(d)
	defer h.Close()

	// the wizard page renders
	page := get(t, h, "/")
	if !strings.Contains(page, "Welcome to LeetOffice") || !strings.Contains(page, "Start a new team") {
		t.Fatalf("wizard page wrong:\n%.300s", page)
	}

	// complete setup: local-only team
	body := post(t, h, "/setup/local", `{"actor":"josh"}`)
	if body["ok"] != true || body["redirect"] != "/" {
		t.Fatalf("setup response: %#v", body)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatal("config not written")
	}
	// the daemon now serves the workspace (handler swapped, no restart)
	home := get(t, h, "/")
	if !strings.Contains(home, "Channels") || !strings.Contains(home, "/api/state") {
		t.Fatalf("chat shell not live after wizard:\n%.300s", home)
	}
	settings := get(t, h, "/settings")
	if !strings.Contains(settings, "make always-on") {
		t.Fatalf("settings page missing service button:\n%.300s", settings)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil || cfg.Actor != "human:josh" {
		t.Fatalf("config after wizard: %v %+v", err, cfg)
	}

	// a brand-new team lands on a living workspace, not empty lists
	state := get(t, h, "/api/state?channel=general")
	if !strings.Contains(state, "team channel") {
		t.Fatalf("welcome message missing from #general: %s", state)
	}
	docs := get(t, h, "/docs")
	if !strings.Contains(docs, "welcome") {
		t.Fatalf("welcome doc missing: %.300s", docs)
	}

	// coordinator path through the wizard too
	d2 := &Daemon{cfgPath: filepath.Join(dir, "n2.json"), ctx: context.Background()}
	d2.mu.Lock()
	d2.handler = d2.setupHandler()
	d2.mu.Unlock()
	h2 := httptest.NewServer(d2)
	defer h2.Close()
	res := post(t, h2, "/setup/start", `{"actor":"maya"}`)
	if res["ok"] != true || res["enrollment_secret"] == "" {
		t.Fatalf("start-team response: %#v", res)
	}
}

func TestSetupValidation(t *testing.T) {
	dir := t.TempDir()
	d := &Daemon{cfgPath: filepath.Join(dir, "n.json"), ctx: context.Background()}
	d.mu.Lock()
	d.handler = d.setupHandler()
	d.mu.Unlock()
	h := httptest.NewServer(d)
	defer h.Close()

	code := postStatus(t, h, "/setup/local", `{"actor":""}`)
	if code != 400 {
		t.Fatalf("missing name accepted: %d", code)
	}
	code = postStatus(t, h, "/setup/join", `{"actor":"x"}`)
	if code != 400 {
		t.Fatalf("join without coordinator accepted: %d", code)
	}
}

func TestServiceFileContent(t *testing.T) {
	plist, err := plistContent(serviceLabel, "/usr/local/bin/leetd", "/tmp/n.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dev.leetoffice.leetd", "<key>RunAtLoad</key><true/>",
		"<string>serve</string>", "/usr/local/bin/leetd"} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist missing %q:\n%s", want, plist)
		}
	}
	unit := unitContent(serviceLabel, "/opt/leetd", "/tmp/n.json")
	for _, want := range []string{"ExecStart=/opt/leetd serve --config /tmp/n.json", "Restart=on-failure"} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}

func get(t *testing.T, h *httptest.Server, path string) string {
	t.Helper()
	res, err := h.Client().Get(h.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if err != nil {
			break
		}
	}
	return string(raw)
}

func post(t *testing.T, h *httptest.Server, path, jsonBody string) map[string]any {
	t.Helper()
	res, err := h.Client().Post(h.URL+path, "application/json", strings.NewReader(jsonBody))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("non-JSON response from %s: %v", path, err)
	}
	return out
}

func postStatus(t *testing.T, h *httptest.Server, path, jsonBody string) int {
	t.Helper()
	res, err := h.Client().Post(h.URL+path, "application/json", strings.NewReader(jsonBody))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}
