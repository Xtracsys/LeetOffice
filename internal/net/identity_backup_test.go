package net

import (
	"os"
	"path/filepath"
	"testing"
)

// FR-017: re-saving an identity must preserve the previous one in a
// timestamped sibling backup; only the newest identityBackupsKeep survive.
func TestIdentitySaveBacksUpPreviousIdentity(t *testing.T) {
	ca, err := CreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "identity")

	first, err := ca.Issue("node-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Save(dir); err != nil {
		t.Fatal(err)
	}
	oldCert, err := os.ReadFile(filepath.Join(dir, nodeCert))
	if err != nil {
		t.Fatal(err)
	}

	// No backup for a fresh create.
	if baks, _ := filepath.Glob(dir + ".bak-*"); len(baks) != 0 {
		t.Fatalf("fresh create produced backups: %v", baks)
	}

	second, err := ca.Issue("node-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Save(dir); err != nil {
		t.Fatal(err)
	}
	baks, _ := filepath.Glob(dir + ".bak-*")
	if len(baks) != 1 {
		t.Fatalf("overwriting produced %d backups, want 1", len(baks))
	}
	bakCert, err := os.ReadFile(filepath.Join(baks[0], nodeCert))
	if err != nil {
		t.Fatal(err)
	}
	if string(bakCert) != string(oldCert) {
		t.Fatal("backup does not hold the previous identity's certificate")
	}
	newCert, err := os.ReadFile(filepath.Join(dir, nodeCert))
	if err != nil {
		t.Fatal(err)
	}
	if string(newCert) == string(oldCert) {
		t.Fatal("identity was not actually replaced")
	}
}

func TestIdentitySavePrunesOldBackups(t *testing.T) {
	ca, err := CreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "identity")
	for i := 0; i < identityBackupsKeep+3; i++ {
		id, err := ca.Issue("node")
		if err != nil {
			t.Fatal(err)
		}
		if err := id.Save(dir); err != nil {
			t.Fatal(err)
		}
		// stamps have nanosecond precision, but a fast loop can still land
		// two saves on one stamp — CopyFS fails on an existing target then,
		// which Save must surface rather than silently skip the backup.
		baks, _ := filepath.Glob(dir + ".bak-*")
		if len(baks) > identityBackupsKeep {
			t.Fatalf("pass %d: %d backups, want <= %d", i, len(baks), identityBackupsKeep)
		}
	}
	baks, _ := filepath.Glob(dir + ".bak-*")
	if len(baks) > identityBackupsKeep {
		t.Fatalf("%d backups survived, want <= %d", len(baks), identityBackupsKeep)
	}
}
