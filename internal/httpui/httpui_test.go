package httpui

import (
	"io"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leetoffice/internal/config"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

func newUI(t *testing.T) (*UI, *store.Store, *leetSync.Repo) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := leetSync.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(dir, "human:josh")
	return &UI{Store: s, Repo: repo, Config: cfg}, s, repo
}

func get(t *testing.T, h *httptest.Server, path string) string {
	t.Helper()
	res, err := h.Client().Get(h.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return string(raw)
}

func TestHomeListsDocs(t *testing.T) {
	ui, s, _ := newUI(t)
	d := store.NewDoc(store.TypeDoc, "spec", "Spec")
	d.AddParagraph("content one")
	if err := s.Save(d, "human:josh"); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(ui.Handler())
	defer h.Close()
	page := get(t, h, "/docs")
	if !strings.Contains(page, "spec") || !strings.Contains(page, "Spec") {
		t.Fatalf("home page missing doc row:\n%s", page)
	}
	if !strings.Contains(page, "human:josh") {
		t.Fatal("home page missing actor")
	}
}

func TestCreateEditDoc(t *testing.T) {
	ui, s, repo := newUI(t)
	h := httptest.NewServer(ui.Handler())
	defer h.Close()

	// create
	form := url.Values{"type": {"doc"}, "slug": {"notes"}, "title": {"Notes"}, "body": {"first"}}
	res, err := h.Client().PostForm(h.URL+"/doc/new", form)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	d, err := s.Load("notes")
	if err != nil || len(d.Blocks) != 1 || d.Blocks[0].Content != "first" {
		t.Fatalf("create failed: %v %+v", err, d)
	}

	// edit: change block + add a block
	form = url.Values{}
	form.Set("block_"+d.Blocks[0].ID, "first (edited)")
	form.Set("new_type", "paragraph")
	form.Set("new_content", "second block")
	res2, err := h.Client().PostForm(h.URL+"/doc/notes", form)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	d, _ = s.Load("notes")
	if len(d.Blocks) != 2 || d.Blocks[0].Content != "first (edited)" {
		t.Fatalf("edit failed: %+v", d.Blocks)
	}
	if d.Audit.LastEditor != "human:josh" {
		t.Fatalf("edit not attributed: %+v", d.Audit)
	}
	// the edit landed as a git commit
	entries, err := repo.AuditLog("docs/notes.html", time.Time{}, "human:josh", 5)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no attributed commit: %v %+v", err, entries)
	}
}

func TestConflictRendering(t *testing.T) {
	ui, s, _ := newUI(t)
	d := store.NewDoc(store.TypeDoc, "conf", "Conf")
	blk := d.AddParagraph("merged version")
	blk.Meta = map[string]any{"conflict": true}
	if err := s.Save(d, "human:josh"); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(ui.Handler())
	defer h.Close()
	page := get(t, h, "/doc/conf")
	if !strings.Contains(page, "conflicting edit") {
		t.Fatalf("conflict marker not rendered:\n%s", page)
	}
}

func TestAgentsPage(t *testing.T) {
	ui, _, _ := newUI(t)
	ui.BinaryPath = "/usr/local/bin/leetd"
	ui.CfgPath = "/tmp/node.json"
	h := httptest.NewServer(ui.Handler())
	defer h.Close()
	page := get(t, h, "/agents")
	if !strings.Contains(page, "mcpServers") || !strings.Contains(page, "/usr/local/bin/leetd") {
		t.Fatalf("agents page missing snippet:\n%.400s", page)
	}
	if !strings.Contains(page, "copy configuration") {
		t.Fatal("copy button missing")
	}
}

func TestChatShellAndAPI(t *testing.T) {
	ui, st, _ := newUI(t)
	h := httptest.NewServer(ui.Handler())
	defer h.Close()

	// the shell is the home page
	page := get(t, h, "/")
	if !strings.Contains(page, "Channels") || !strings.Contains(page, "composer") ||
		!strings.Contains(page, "/api/state") {
		t.Fatalf("chat shell wrong:\n%.300s", page)
	}

	// send via the API exactly as the composer does
	res, err := h.Client().Post(h.URL+"/api/send", "application/json",
		strings.NewReader(`{"channel":"general","text":"hello from the UI"}`))
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("api send: %v %v", err, res)
	}
	res.Body.Close()

	// state shows the channel, the message, and the sender's presence
	state := get(t, h, "/api/state?channel=general")
	if !strings.Contains(state, `"slug":"general"`) ||
		!strings.Contains(state, "hello from the UI") ||
		!strings.Contains(state, `"human:josh":"`) {
		t.Fatalf("api state: %s", state)
	}
	d, err := st.Load("general")
	if err != nil || d.Type != store.TypeChannel {
		t.Fatalf("channel doc: %v", err)
	}
	// empty messages are rejected
	res2, _ := h.Client().Post(h.URL+"/api/send", "application/json",
		strings.NewReader(`{"channel":"general","text":"  "}`))
	if res2.StatusCode != 400 {
		t.Fatalf("empty accepted: %d", res2.StatusCode)
	}
	res2.Body.Close()
}

func TestAuditPageShowsHistory(t *testing.T) {
	ui, _, repo := newUI(t)
	d := store.NewDoc(store.TypeDoc, "spec", "Spec")
	d.AddParagraph("content")
	if err := ui.Store.Save(d, "human:josh"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CommitAll("human:josh", "write spec"); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(ui.Handler())
	defer h.Close()
	page := get(t, h, "/audit")
	if !strings.Contains(page, "human:josh") || !strings.Contains(page, "write spec") {
		t.Fatalf("audit page missing history:\n%.400s", page)
	}
	filtered := get(t, h, "/audit?actor=agent:nobody")
	if strings.Contains(filtered, "write spec") {
		t.Fatal("actor filter not applied")
	}
}

func TestSettingsPageAndInvite(t *testing.T) {
	ui, _, _ := newUI(t)
	ui.CfgPath = filepath.Join(t.TempDir(), "node.json")
	ui.Config.EnrollmentSecret = "firstcode123"
	ui.Config.Role = "coordinator"
	var live []string
	ui.RotateEnrollment = func(secret string) { live = append(live, secret) }
	cfg := *ui.Config
	if err := cfg.Save(ui.CfgPath); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(ui.Handler())
	defer h.Close()

	// coordinator sees the invite code and can regenerate it
	page := get(t, h, "/settings")
	if !strings.Contains(page, "firstcode123") || !strings.Contains(page, "Team invite") {
		t.Fatalf("settings missing invite:\n%.400s", page)
	}
	res, err := h.Client().PostForm(h.URL+"/settings/invite", nil)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("regen: %v %v", err, res)
	}
	res.Body.Close()
	page2 := get(t, h, "/settings")
	if strings.Contains(page2, "firstcode123") {
		t.Fatal("old invite still shown after regeneration")
	}
	if len(live) != 1 || live[0] == "" || live[0] == "firstcode123" {
		t.Fatalf("RotateEnrollment not called with the new secret: %v", live)
	}
	if !strings.Contains(page2, live[0]) {
		t.Fatal("settings does not show the secret passed to the live server")
	}

	// saving identity + cadence persists
	res2, err := h.Client().PostForm(h.URL+"/settings", url.Values{
		"actor": {"maya"}, "sync_every_sec": {"15"},
		"ollama_base": {ui.Config.Ollama.BaseURL}, "ollama_model": {ui.Config.Ollama.Model}})
	if err != nil || res2.StatusCode != 200 {
		t.Fatalf("save: %v %v", err, res2)
	}
	res2.Body.Close()
	saved := get(t, h, "/settings")
	if !strings.Contains(saved, "value=\"maya\"") || !strings.Contains(saved, `value="15"`) {
		t.Fatalf("settings not persisted:\n%.400s", saved)
	}
}
