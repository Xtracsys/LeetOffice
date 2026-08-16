package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocRoundTrip(t *testing.T) {
	d := NewDoc(TypeDoc, "imaging-runbook", "XtracBox Imaging Runbook")
	d.AddParagraph("Boot the target from the Ventoy USB.")
	b, err := d.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	parsed, err := ParseDoc(b)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if parsed.ID != d.ID || len(parsed.Blocks) != 1 {
		t.Fatalf("round-trip mismatch: id=%s blocks=%d", parsed.ID, len(parsed.Blocks))
	}
	if parsed.Schema != SchemaURL {
		t.Fatalf("schema = %s", parsed.Schema)
	}
}

func TestHTMLRoundTrip(t *testing.T) {
	d := NewDoc(TypeDoc, "runbook", "Runbook")
	d.AddParagraph("Boot the target from the <Ventoy> USB & flash.")
	d.AddBlock(Block{Type: BlockHeading, Content: "Notes", Level: 2})
	page, err := RenderDoc(d)
	if err != nil {
		t.Fatalf("RenderDoc: %v", err)
	}
	parsed, err := ExtractDoc(page)
	if err != nil {
		t.Fatalf("ExtractDoc: %v", err)
	}
	if parsed.ID != d.ID || len(parsed.Blocks) != 2 ||
		parsed.Blocks[0].Content != d.Blocks[0].Content {
		t.Fatalf("HTML round-trip mismatch")
	}
	// re-render the parse — lossless per the Phase 1 gate
	page2, err := RenderDoc(parsed)
	if err != nil {
		t.Fatalf("re-render: %v", err)
	}
	p2, err := ExtractDoc(page2)
	if err != nil || p2.ID != parsed.ID || len(p2.Blocks) != 2 {
		t.Fatalf("re-render round-trip failed")
	}
}

func TestStoreSaveLoadIndex(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	d := NewDoc(TypeDoc, "imaging-runbook", "Imaging Runbook")
	d.Tags = []string{"imaging"}
	d.AddParagraph("Boot from USB.")
	if err := s.Save(d, "human:josh"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load("imaging-runbook")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != d.ID || got.Audit.LastEditor != "human:josh" {
		t.Fatalf("load mismatch: %+v", got.Audit)
	}
	idx, err := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if err != nil {
		t.Fatalf("INDEX.md: %v", err)
	}
	if !strings.Contains(string(idx), "| imaging-runbook | doc | Imaging Runbook |") {
		t.Fatalf("INDEX.md missing row:\n%s", idx)
	}
	// task goes to its own folder
	task := NewDoc(TypeTask, "t-1", "Task One")
	if err := s.Save(task, "agent:hermes"); err != nil {
		t.Fatalf("Save task: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks", "t-1.html")); err != nil {
		t.Fatalf("task file not in tasks/: %v", err)
	}
}

func TestBidirectionalLink(t *testing.T) {
	doc := NewDoc(TypeDoc, "runbook", "Runbook")
	blk := doc.AddParagraph("step one")
	task := NewDoc(TypeTask, "task-1", "Verify imaging")
	tblk := task.AddBlock(Block{Type: BlockTaskItem, Content: "verify"})

	if err := AddLink(doc, blk.ID, task, tblk.ID, "links to"); err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	if len(doc.Block(blk.ID).Links) != 1 || doc.Block(blk.ID).Links[0].Dir != "out" {
		t.Fatalf("source block missing out-link")
	}
	if len(task.Block(tblk.ID).Links) != 1 || task.Block(tblk.ID).Links[0].Dir != "in" {
		t.Fatalf("target block missing in-backlink")
	}
}

func TestMergeDifferentBlocks(t *testing.T) {
	base := NewDoc(TypeDoc, "m", "M")
	b1 := base.AddParagraph("one")
	b2 := base.AddParagraph("two")

	ours, _ := cloneDoc(base)
	ours.Block(b1.ID).Content = "one (ours)"
	theirs, _ := cloneDoc(base)
	theirs.Block(b2.ID).Content = "two (theirs)"

	merged, conflicts := MergeDocs(base, ours, theirs)
	if len(conflicts) != 0 {
		t.Fatalf("expected clean merge, got %d conflicts", len(conflicts))
	}
	if merged.Block(b1.ID).Content != "one (ours)" || merged.Block(b2.ID).Content != "two (theirs)" {
		t.Fatalf("merge lost an edit: %v", merged.Blocks)
	}
}

func TestMergeSameBlockConflict(t *testing.T) {
	base := NewDoc(TypeDoc, "m", "M")
	blk := base.AddParagraph("original")

	ours, _ := cloneDoc(base)
	ours.Block(blk.ID).Content = "ours edit"
	theirs, _ := cloneDoc(base)
	theirs.Block(blk.ID).Content = "theirs edit"

	merged, conflicts := MergeDocs(base, ours, theirs)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	var contents []string
	for _, b := range merged.Blocks {
		contents = append(contents, b.Content)
	}
	if !contains(contents, "ours edit") || !contains(contents, "theirs edit") {
		t.Fatalf("conflict dropped a version: %v", contents)
	}
	for _, b := range merged.Blocks {
		if b.Content == "ours edit" && b.Meta["conflict"] != true {
			t.Fatal("later (ours) version not conflict-flagged")
		}
	}
}

func TestMergeAddOnBothSides(t *testing.T) {
	base := NewDoc(TypeDoc, "m", "M")
	ours, _ := cloneDoc(base)
	ours.AddParagraph("added by ours")
	theirs, _ := cloneDoc(base)
	theirs.AddParagraph("added by theirs")
	merged, conflicts := MergeDocs(base, ours, theirs)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %d", len(conflicts))
	}
	if len(merged.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(merged.Blocks))
	}
}

func cloneDoc(d *Doc) (*Doc, error) {
	b, err := d.Bytes()
	if err != nil {
		return nil, err
	}
	return ParseDoc(b)
}

func TestFieldEncryption(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	k, err := NewKeyringFromSeed(seed)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	ev, err := k.Encrypt("sensitive-value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !ev.Enc {
		t.Fatal("not marked encrypted")
	}
	if containsString(ev.Data, "sensitive") {
		t.Fatal("ciphertext leaked plaintext")
	}
	got, err := k.Decrypt(ev)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "sensitive-value" {
		t.Fatalf("decrypt = %q", got)
	}
}

func contains(list []string, sub string) bool { // slice variant
	for _, s := range list {
		if s == sub {
			return true
		}
	}
	return false
}

var _ = containsString

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
