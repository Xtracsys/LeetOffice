package httpui

import (
	"io"
	"net/http/httptest"
	"net/url"
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
	page := get(t, h, "/")
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

