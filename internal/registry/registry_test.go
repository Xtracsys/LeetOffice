package registry

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"leetoffice/internal/sync"
)

// fixtureRepo creates a git-backed registry root.
func fixtureRepo(t *testing.T) (string, *sync.Repo) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := sync.Init(root)
	if err != nil {
		t.Fatalf("sync.Init: %v", err)
	}
	return root, repo
}

// srcEntry writes a source folder outside the registry to import from.
func srcEntry(t *testing.T, m Manifest) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), m.Name)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# "+m.Name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestImportAndLoad(t *testing.T) {
	root, repo := fixtureRepo(t)
	src := srcEntry(t, Manifest{Name: "demo-skill", Kind: "skill", Version: "1.0.0", Stability: Experimental})

	e, err := Import(root, src, "human:josh", repo)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if e.Dir != filepath.Join("skills", "demo-skill") {
		t.Fatalf("entry dir = %s", e.Dir)
	}
	entries, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 1 || entries[0].Manifest.Name != "demo-skill" {
		t.Fatalf("Load: %+v", entries)
	}
	// the import was committed (git audit trail)
	log, err := repo.AuditLog("", time.Time{}, "", 10)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}
	found := false
	for _, e := range log {
		if containsStr(e.Msg, "registry: import demo-skill@1.0.0") {
			found = true
		}
	}
	if !found {
		t.Fatalf("import not committed: %+v", log)
	}
}

func TestDuplicateImportRejected(t *testing.T) {
	root, repo := fixtureRepo(t)
	src := srcEntry(t, Manifest{Name: "dup", Kind: "tool", Version: "0.2.0", Stability: Experimental})
	if _, err := Import(root, src, "human:josh", repo); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := Import(root, src, "human:josh", repo); err == nil {
		t.Fatal("duplicate import accepted")
	}
}

func TestAutoPromoteAtThreshold(t *testing.T) {
	root, repo := fixtureRepo(t)
	src := srcEntry(t, Manifest{Name: "promote-me", Kind: "skill", Version: "0.3.0", Stability: Experimental, Threshold: 3})
	if _, err := Import(root, src, "human:josh", repo); err != nil {
		t.Fatalf("import: %v", err)
	}

	for i := 1; i <= 2; i++ {
		e, err := RecordUse(root, "promote-me", true, "human:josh", repo)
		if err != nil {
			t.Fatalf("use %d: %v", i, err)
		}
		if e.Manifest.Stability != Experimental {
			t.Fatalf("promoted early at use %d", i)
		}
	}
	e, err := RecordUse(root, "promote-me", true, "human:josh", repo)
	if err != nil {
		t.Fatalf("third use: %v", err)
	}
	if e.Manifest.Stability != Stable {
		t.Fatalf("not promoted at threshold: %+v", e.Manifest)
	}
	log, _ := repo.AuditLog("", time.Time{}, "", 20)
	promoted := false
	for _, entry := range log {
		if containsStr(entry.Msg, "registry: promote promote-me to stable") {
			promoted = true
		}
	}
	if !promoted {
		t.Fatal("promotion commit missing")
	}

	// a failure resets the counter
	e, err = RecordUse(root, "promote-me", false, "human:josh", repo)
	if err != nil {
		t.Fatalf("failure: %v", err)
	}
	if e.Manifest.CleanUses != 0 {
		t.Fatalf("clean_uses not reset: %d", e.Manifest.CleanUses)
	}
}

func TestDefaultThreshold(t *testing.T) {
	root, _ := fixtureRepo(t)
	src := srcEntry(t, Manifest{Name: "slow-burn", Kind: "tool", Version: "1.1.0", Stability: Experimental})
	if _, err := Import(root, src, "human:josh", nil); err != nil {
		t.Fatalf("import: %v", err)
	}
	for i := 0; i < DefaultThreshold-1; i++ {
		if _, err := RecordUse(root, "slow-burn", true, "human:josh", nil); err != nil {
			t.Fatal(err)
		}
	}
	e, err := RecordUse(root, "slow-burn", true, "human:josh", nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Manifest.Stability != Stable {
		t.Fatalf("default threshold not applied: %+v", e.Manifest)
	}
}

func TestExportZip(t *testing.T) {
	root, repo := fixtureRepo(t)
	src := srcEntry(t, Manifest{Name: "zipme", Kind: "skill", Version: "2.0.0", Stability: Stable})
	if _, err := Import(root, src, "human:josh", repo); err != nil {
		t.Fatalf("import: %v", err)
	}
	e, err := Find(root, "zipme")
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "export")
	out, err := Export(e, root, dest)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if !containsStr(join(names), "zipme/manifest.json") || !containsStr(join(names), "zipme/SKILL.md") {
		t.Fatalf("zip contents wrong: %v", names)
	}
}

func TestLoadSkipsNonRegistryFolders(t *testing.T) {
	root, _ := fixtureRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "skills", "not-an-entry"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "loose.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %+v", entries)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func join(list []string) string {
	out := ""
	for _, s := range list {
		out += s + "\n"
	}
	return out
}
