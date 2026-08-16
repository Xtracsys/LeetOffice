package rag

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leetoffice/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return s
}

func writeMemory(t *testing.T, s *store.Store, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.Root, "MEMORY.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
}

func TestKeywordSearchBoostsMemory(t *testing.T) {
	s := newStore(t)
	d := store.NewDoc(store.TypeDoc, "field-notes", "Field notes")
	blk := d.AddParagraph("boot the target from the ventoy usb")
	if err := s.Save(d, "human:josh"); err != nil {
		t.Fatalf("save: %v", err)
	}
	writeMemory(t, s, "# Team Memory\n\n## Open tasks\n- ventoy boot checklist\n")

	hits, err := Search(s, "ventoy", "", nil, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2: %+v", len(hits), hits)
	}
	// equally-good matches, but the MEMORY bullet must outrank the plain block
	if hits[0].Slug != "MEMORY" || hits[0].Score <= hits[1].Score {
		t.Fatalf("memory hit not boosted to top: %+v", hits)
	}
	if hits[1].BlockID != blk.ID || !strings.Contains(hits[1].Snippet, "ventoy") {
		t.Fatalf("plain block missing or bad snippet: %+v", hits)
	}
	if hits[0].DocID != "MEMORY" || hits[0].Title != "Team Memory" {
		t.Fatalf("memory pseudo-block shape: %+v", hits[0])
	}
}

func TestEncryptedBlocksNeverIndexed(t *testing.T) {
	s := newStore(t)
	d := store.NewDoc(store.TypeDoc, "secrets", "Secrets")
	plain := d.AddParagraph("ventoy appears here")
	d.AddBlock(store.Block{Type: store.BlockParagraph, Content: "ventoy hidden at rest", Meta: map[string]any{"enc": true}})
	ev, err := json.Marshal(store.EncryptedValue{Enc: true, Alg: "AES-256-GCM", Data: "dmVudG95"})
	if err != nil {
		t.Fatalf("marshal encrypted value: %v", err)
	}
	d.AddParagraph(string(ev))
	if err := s.Save(d, "human:josh"); err != nil {
		t.Fatalf("save: %v", err)
	}

	hits, err := Search(s, "ventoy", "", nil, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].BlockID != plain.ID {
		t.Fatalf("want exactly the plain block, got: %+v", hits)
	}
	for _, h := range hits {
		if strings.Contains(h.Snippet, "hidden at rest") || strings.Contains(h.Snippet, "dmVudG95") {
			t.Fatalf("encrypted block leaked into results: %+v", h)
		}
	}
}

func TestTypeAndTagFilters(t *testing.T) {
	s := newStore(t)
	a := store.NewDoc(store.TypeDoc, "runbook", "Ops Runbook")
	a.Tags = []string{"ops"}
	a.AddParagraph("kubernetes upgrade steps")
	if err := s.Save(a, "human:josh"); err != nil {
		t.Fatalf("save a: %v", err)
	}
	b := store.NewDoc(store.TypeTask, "kube-task", "Kubernetes task")
	b.AddParagraph("kubernetes upgrade task")
	if err := s.Save(b, "human:josh"); err != nil {
		t.Fatalf("save b: %v", err)
	}

	hits, err := Search(s, "kubernetes", "task", nil, 0)
	if err != nil {
		t.Fatalf("Search type: %v", err)
	}
	if len(hits) == 0 || hits[0].Slug != "kube-task" {
		t.Fatalf("type filter failed: %+v", hits)
	}
	for _, h := range hits {
		if h.Slug != "kube-task" {
			t.Fatalf("non-task hit leaked: %+v", h)
		}
	}

	hits, err = Search(s, "kubernetes", "", []string{"ops"}, 0)
	if err != nil {
		t.Fatalf("Search tags: %v", err)
	}
	if len(hits) == 0 || hits[0].Slug != "runbook" {
		t.Fatalf("tag filter failed: %+v", hits)
	}
	for _, h := range hits {
		if h.Slug != "runbook" {
			t.Fatalf("non-tagged hit leaked: %+v", h)
		}
	}
}

func TestCuratedDocsGetBoost(t *testing.T) {
	s := newStore(t)
	plain := store.NewDoc(store.TypeDoc, "plain-notes", "Plain notes")
	plain.AddParagraph("kubernetes upgrade steps")
	summary := store.NewDoc(store.TypeDoc, "summary-notes", "Meeting summary")
	summary.Tags = []string{"summary"}
	summary.AddParagraph("kubernetes upgrade steps")
	if err := s.Save(plain, "human:josh"); err != nil {
		t.Fatalf("save plain: %v", err)
	}
	if err := s.Save(summary, "human:josh"); err != nil {
		t.Fatalf("save summary: %v", err)
	}

	hits, err := Search(s, "kubernetes", "", nil, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 || hits[0].Slug != "summary-notes" {
		t.Fatalf("summary doc not boosted: %+v", hits)
	}
}

func TestOllamaUnavailableFallsBackToKeyword(t *testing.T) {
	s := newStore(t)
	d := store.NewDoc(store.TypeDoc, "field-notes", "Field notes")
	d.AddParagraph("boot the target from the ventoy usb")
	if err := s.Save(d, "human:josh"); err != nil {
		t.Fatalf("save: %v", err)
	}

	o := &Ollama{BaseURL: "http://127.0.0.1:1"} // closed port
	if o.Available() {
		t.Fatal("expected Available() false on a closed port")
	}
	hits, err := o.SearchSemantic(s, "ventoy usb", "", nil, 0)
	if err != nil {
		t.Fatalf("SearchSemantic fallback errored: %v", err)
	}
	kw, err := Search(s, "ventoy usb", "", nil, 0)
	if err != nil {
		t.Fatalf("keyword Search: %v", err)
	}
	if len(hits) != len(kw) || len(hits) == 0 {
		t.Fatalf("fallback mismatch: semantic=%d keyword=%d", len(hits), len(kw))
	}
	for i := range hits {
		if hits[i].BlockID != kw[i].BlockID || hits[i].Slug != kw[i].Slug {
			t.Fatalf("fallback results differ at %d: %+v vs %+v", i, hits[i], kw[i])
		}
	}
}

func TestSearchSemanticViaFakeOllama(t *testing.T) {
	s := newStore(t)
	d := store.NewDoc(store.TypeDoc, "notes", "Notes")
	relevant := d.AddParagraph("boot from ventoy usb")
	d.AddBlock(store.Block{Type: store.BlockParagraph, Content: "ventoy secret at rest", Meta: map[string]any{"enc": true}})
	if err := s.Save(d, "human:josh"); err != nil {
		t.Fatalf("save: %v", err)
	}
	writeMemory(t, s, "# Team Memory\n- ventoy boot checklist\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[]}`))
		case "/api/embed":
			var req struct {
				Model string   `json:"model"`
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" || len(req.Input) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// deterministic embeddings: [term hits, 1] — query "ventoy"
			// aligns with the blocks that mention it
			resp := struct {
				Embeddings [][]float64 `json:"embeddings"`
			}{}
			for _, in := range req.Input {
				n := strings.Count(strings.ToLower(in), "ventoy")
				resp.Embeddings = append(resp.Embeddings, []float64{float64(n), 1})
			}
			json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL}
	if !o.Available() {
		t.Fatal("fake ollama not available")
	}
	hits, err := o.SearchSemantic(s, "ventoy", "", nil, 0)
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no semantic hits")
	}
	if hits[0].Slug != "MEMORY" {
		t.Fatalf("memory bullet not boosted to top: %+v", hits)
	}
	found := false
	for _, h := range hits {
		if h.BlockID == relevant.ID {
			found = true
		}
		if strings.Contains(h.Snippet, "secret") {
			t.Fatalf("encrypted block returned by semantic search: %+v", h)
		}
	}
	if !found {
		t.Fatalf("relevant block missing from semantic hits: %+v", hits)
	}
}
