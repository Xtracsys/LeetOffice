package net

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordAndListIssued(t *testing.T) {
	dir := t.TempDir()
	ca, err := CreateCA(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := ca.Issue("Hivemind.local")
	if err != nil {
		t.Fatal(err)
	}
	nodeDir := filepath.Join(dir, "node")
	if err := id.Save(nodeDir); err != nil {
		t.Fatal(err)
	}
	pem, err := os.ReadFile(filepath.Join(nodeDir, "node.crt"))
	if err != nil {
		t.Fatal(err)
	}
	issued := filepath.Join(dir, IssuedDir)
	if err := RecordIssued(issued, "Hivemind.local", pem); err != nil {
		t.Fatal(err)
	}
	got := ListIssued(issued)
	if len(got) != 1 || got[0].NodeID != "Hivemind.local" || got[0].Fingerprint == "" {
		t.Fatalf("ListIssued = %+v", got)
	}
}
