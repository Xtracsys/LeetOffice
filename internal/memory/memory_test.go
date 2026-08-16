package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leetoffice/internal/store"
	"leetoffice/internal/sync"
)

func newFixture(t *testing.T) (*store.Store, *sync.Repo) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	r, err := sync.Init(dir)
	if err != nil {
		t.Fatalf("sync.Init: %v", err)
	}
	return s, r
}

func TestSynthesizeWritesMemory(t *testing.T) {
	s, repo := newFixture(t)

	open := store.NewDoc(store.TypeTask, "ship-v1", "Ship v1 release")
	open.AddBlock(store.Block{Type: store.BlockTaskItem, Content: "cut the release branch"})
	open.AddBlock(store.Block{Type: store.BlockTaskItem, Content: "already done", Meta: map[string]any{"done": true}})
	if err := s.Save(open, "human:josh"); err != nil {
		t.Fatalf("save open task: %v", err)
	}
	closed := store.NewDoc(store.TypeTask, "closed-task", "Closed thing")
	closed.AddBlock(store.Block{Type: store.BlockTaskItem, Content: "finished", Meta: map[string]any{"done": true}})
	if err := s.Save(closed, "human:josh"); err != nil {
		t.Fatalf("save closed task: %v", err)
	}
	dec := store.NewDoc(store.TypeDoc, "storage-choice", "We chose SQLite for v1")
	dec.Tags = []string{"decision"}
	dec.AddParagraph("Rationale: pure Go, no cgo.")
	if err := s.Save(dec, "human:josh"); err != nil {
		t.Fatalf("save decision: %v", err)
	}
	if _, err := repo.CommitAll("human:josh", "seed store"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if err := Synthesize(s, repo, "agent:hermes"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(s.Root, "MEMORY.md"))
	if err != nil {
		t.Fatalf("MEMORY.md: %v", err)
	}
	mem := string(raw)
	for _, want := range []string{
		"# Team Memory\n",
		"_updated ",
		"## In flight",
		"## Decisions",
		"## Open tasks",
		"## Owners / context",
		"- [ ] Ship v1 release (tasks/ship-v1) — updated ",
		"We chose SQLite for v1",
	} {
		if !strings.Contains(mem, want) {
			t.Fatalf("MEMORY.md missing %q:\n%s", want, mem)
		}
	}
	openSection, _, _ := strings.Cut(strings.SplitN(mem, "## Open tasks\n", 2)[1], "\n## ")
	if strings.Contains(openSection, "Closed thing") {
		t.Fatalf("fully-done task listed as open:\n%s", mem)
	}

	// MEMORY.md is a root artifact, never a store doc.
	docs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, d := range docs {
		if d.Slug == "MEMORY" || d.Title == "Team Memory" {
			t.Fatalf("MEMORY.md leaked into the store: %+v", d)
		}
	}
	if _, err := s.Load("MEMORY"); err == nil {
		t.Fatal("MEMORY.md resolved as a store doc")
	}
}

func TestSynthesizeUpdatesAndCommitsAttributed(t *testing.T) {
	s, repo := newFixture(t)

	first := store.NewDoc(store.TypeTask, "first", "First task")
	first.AddBlock(store.Block{Type: store.BlockTaskItem, Content: "do it"})
	if err := s.Save(first, "human:josh"); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if _, err := repo.CommitAll("human:josh", "seed store"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := Synthesize(s, repo, "agent:hermes"); err != nil {
		t.Fatalf("first Synthesize: %v", err)
	}

	second := store.NewDoc(store.TypeTask, "second", "Second task")
	second.AddBlock(store.Block{Type: store.BlockTaskItem, Content: "also do it"})
	if err := s.Save(second, "human:josh"); err != nil {
		t.Fatalf("save second: %v", err)
	}
	if err := Synthesize(s, repo, "agent:hermes"); err != nil {
		t.Fatalf("second Synthesize: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(s.Root, "MEMORY.md"))
	if err != nil {
		t.Fatalf("MEMORY.md: %v", err)
	}
	if !strings.Contains(string(raw), "- [ ] Second task (tasks/second)") {
		t.Fatalf("MEMORY.md not updated after change:\n%s", raw)
	}

	entries, err := repo.AuditLog("MEMORY.md", time.Time{}, "", 0)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.TrimSpace(e.Msg) == "memory: synthesize" && e.Actor == "agent:hermes" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no memory commit attributed to agent:hermes: %+v", entries)
	}
}

func TestDailyDigest(t *testing.T) {
	s, repo := newFixture(t)

	doc := store.NewDoc(store.TypeDoc, "spec", "Spec Doc")
	b1 := doc.AddParagraph("one")
	task := store.NewDoc(store.TypeTask, "t-1", "Task One")
	tbl := task.AddBlock(store.Block{Type: store.BlockTaskItem, Content: "verify"})
	if err := s.Save(doc, "human:josh"); err != nil {
		t.Fatalf("save doc: %v", err)
	}
	if err := s.Save(task, "human:josh"); err != nil {
		t.Fatalf("save task: %v", err)
	}
	if _, err := repo.CommitAll("human:josh", "seed store"); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	if err := store.AddLink(doc, b1.ID, task, tbl.ID, "blocks"); err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	if err := s.Save(doc, "agent:hermes"); err != nil {
		t.Fatalf("save doc link: %v", err)
	}
	if _, err := repo.CommitAll("agent:hermes", "link: spec to task"); err != nil {
		t.Fatalf("commit link: %v", err)
	}

	seed, err := repo.AuditLog("", time.Time{}, "human:josh", 1)
	if err != nil || len(seed) == 0 {
		t.Fatalf("AuditLog seed: %v (%d)", err, len(seed))
	}
	day := seed[0].When

	path, err := DailyDigest(s, repo, day, "human:josh")
	if err != nil {
		t.Fatalf("DailyDigest: %v", err)
	}
	wantPath := filepath.Join(s.Root, "_audit", "DIGEST-"+day.UTC().Format("2006-01-02")+".md")
	if path != wantPath {
		t.Fatalf("path = %s, want %s", path, wantPath)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read digest: %v", err)
	}
	for _, want := range []string{
		"## What changed",
		"## By whom",
		"## New tasks",
		"## Notable links",
		"human:josh",
		"agent:hermes",
		"docs/spec.html",
		"tasks/t-1.html",
		"link: spec to task",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("digest missing %q:\n%s", want, raw)
		}
	}
}

func TestHygiene(t *testing.T) {
	s, _ := newFixture(t)

	// broken link: source saved, target never written to disk
	src := store.NewDoc(store.TypeDoc, "src", "Source")
	sb := src.AddParagraph("links out")
	dst := store.NewDoc(store.TypeDoc, "dst", "Target")
	db := dst.AddParagraph("target block")
	if err := store.AddLink(src, sb.ID, dst, db.ID, "ref"); err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	if err := s.Save(src, "human:josh"); err != nil {
		t.Fatalf("save src: %v", err)
	}

	// stale doc: updated 45 days ago
	stale := store.NewDoc(store.TypeDoc, "old-note", "Old Note")
	stale.AddParagraph("ancient")
	stale.Updated = time.Now().UTC().Add(-45 * 24 * time.Hour).Format(time.RFC3339)
	if err := s.Save(stale, "human:josh"); err != nil {
		t.Fatalf("save stale: %v", err)
	}

	// unindexed file: garbage .html in docs/
	garbage := filepath.Join(s.Root, "docs", "garbage.html")
	if err := os.WriteFile(garbage, []byte("<html><body>not a leet doc</body></html>"), 0o644); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	issues, err := Hygiene(s, 0)
	if err != nil {
		t.Fatalf("Hygiene: %v", err)
	}
	counts := map[string]int{}
	for _, is := range issues {
		counts[is.Kind]++
		switch is.Kind {
		case KindBrokenLink:
			if !strings.Contains(is.Detail, "doc src block ") || !strings.Contains(is.Detail, dst.ID) {
				t.Fatalf("broken-link detail lacks slug/block/target: %q", is.Detail)
			}
		case KindStaleDoc:
			if !strings.Contains(is.Detail, "old-note") {
				t.Fatalf("stale-doc detail lacks slug: %q", is.Detail)
			}
		case KindUnindexedFile:
			if !strings.Contains(is.Detail, "docs/garbage.html") {
				t.Fatalf("unindexed-file detail lacks path: %q", is.Detail)
			}
		}
	}
	if counts[KindBrokenLink] != 1 {
		t.Fatalf("broken-link count = %d, want 1 (issues: %+v)", counts[KindBrokenLink], issues)
	}
	if counts[KindStaleDoc] != 1 {
		t.Fatalf("stale-doc count = %d, want 1 (issues: %+v)", counts[KindStaleDoc], issues)
	}
	if counts[KindUnindexedFile] != 1 {
		t.Fatalf("unindexed-file count = %d, want 1 (issues: %+v)", counts[KindUnindexedFile], issues)
	}
	// reindex happened (INDEX.md is derived)
	if _, err := os.Stat(filepath.Join(s.Root, "INDEX.md")); err != nil {
		t.Fatalf("INDEX.md not regenerated: %v", err)
	}
}
