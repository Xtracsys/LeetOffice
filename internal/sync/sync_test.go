package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leetoffice/internal/store"
)

// twoNodeFixture builds a coordinator bare repo (main share) plus two client
// nodes cloned against it.
func twoNodeFixture(t *testing.T) (bare string, a, b *Repo, sa, sb *store.Store) {
	t.Helper()
	dir := t.TempDir()
	bare = filepath.Join(dir, "main-share.git")
	if _, err := InitBare(bare); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	bareURL := "file://" + bare

	// seed via node A
	sa, a = newNode(t, filepath.Join(dir, "node-a"), bareURL)
	d := store.NewDoc(store.TypeDoc, "spec", "Spec Doc")
	d.AddParagraph("one")
	d.AddParagraph("two")
	if err := sa.Save(d, "human:josh"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := a.CommitAll("human:josh", "seed store"); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	if _, err := a.push("origin"); err != nil {
		t.Fatalf("push seed: %v", err)
	}

	sb, b = newNode(t, filepath.Join(dir, "node-b"), bareURL)
	if _, err := b.Sync("origin", "human:maya"); err != nil {
		t.Fatalf("node-b initial sync: %v", err)
	}
	return bare, a, b, sa, sb
}

func newNode(t *testing.T, dir, bareURL string) (*store.Store, *Repo) {
	t.Helper()
	s, err := store.OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init repo: %v", err)
	}
	if err := r.AddRemote("origin", bareURL); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	return s, r
}

func getBlock(t *testing.T, s *store.Store, slug, content string) *store.Block {
	t.Helper()
	d, err := s.Load(slug)
	if err != nil {
		t.Fatalf("load %s: %v", slug, err)
	}
	for i := range d.Blocks {
		if d.Blocks[i].Content == content {
			return &d.Blocks[i]
		}
	}
	t.Fatalf("block with content %q not found", content)
	return nil
}

func TestDifferentBlocksMergeCleanly(t *testing.T) {
	_, a, b, sa, sb := twoNodeFixture(t)

	// node A edits block "one", node B edits block "two" — concurrently.
	da, _ := sa.Load("spec")
	da.Block(getBlock(t, sa, "spec", "one").ID).Content = "one (A)"
	if err := sa.Save(da, "human:josh"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitAll("human:josh", "A edits one"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Sync("origin", "human:josh"); err != nil {
		t.Fatalf("A sync: %v", err)
	}

	db, _ := sb.Load("spec")
	db.Block(getBlock(t, sb, "spec", "two").ID).Content = "two (B)"
	if err := sb.Save(db, "human:maya"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CommitAll("human:maya", "B edits two"); err != nil {
		t.Fatal(err)
	}
	res, err := b.Sync("origin", "human:maya")
	if err != nil {
		t.Fatalf("B sync: %v", err)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("expected clean merge, got conflicts: %+v", res.Conflicts)
	}
	merged, _ := sb.Load("spec")
	if merged.Block(getBlock(t, sb, "spec", "one (A)").ID) == nil ||
		merged.Block(getBlock(t, sb, "spec", "two (B)").ID) == nil {
		t.Fatalf("merge lost an edit: %+v", merged.Blocks)
	}
	// A catches up
	if _, err := a.Sync("origin", "human:josh"); err != nil {
		t.Fatal(err)
	}
	ma, _ := sa.Load("spec")
	if ma.Block(getBlock(t, sa, "spec", "one (A)").ID) == nil ||
		ma.Block(getBlock(t, sa, "spec", "two (B)").ID) == nil {
		t.Fatal("node A did not catch up")
	}
}

func TestSameBlockConflictKeepsBoth(t *testing.T) {
	_, a, b, sa, sb := twoNodeFixture(t)

	da, _ := sa.Load("spec")
	da.Block(getBlock(t, sa, "spec", "one").ID).Content = "one (A version)"
	_ = sa.Save(da, "human:josh")
	if _, err := a.CommitAll("human:josh", "A edits one"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Sync("origin", "human:josh"); err != nil {
		t.Fatal(err)
	}

	db, _ := sb.Load("spec")
	db.Block(getBlock(t, sb, "spec", "one").ID).Content = "one (B version)"
	_ = sb.Save(db, "human:maya")
	if _, err := b.CommitAll("human:maya", "B edits one"); err != nil {
		t.Fatal(err)
	}
	res, err := b.Sync("origin", "human:maya")
	if err != nil {
		t.Fatalf("B sync: %v", err)
	}
	if len(res.Conflicts) == 0 {
		t.Fatal("expected a conflict flag")
	}
	merged, _ := sb.Load("spec")
	if merged.Block(getBlock(t, sb, "spec", "one (A version)").ID) == nil ||
		merged.Block(getBlock(t, sb, "spec", "one (B version)").ID) == nil {
		t.Fatalf("conflict dropped a version: %+v", merged.Blocks)
	}
	for i := range merged.Blocks {
		if merged.Blocks[i].Content == "one (B version)" && merged.Blocks[i].Meta["conflict"] != true {
			t.Fatal("later conflicting version not flagged")
		}
	}
}

func TestAuditLogAttribution(t *testing.T) {
	_, a, b, sa, _ := twoNodeFixture(t)

	da, _ := sa.Load("spec")
	da.AddParagraph("agent adds a note")
	_ = sa.Save(da, "agent:hermes")
	if _, err := a.CommitAll("agent:hermes", "agent note"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Sync("origin", "agent:hermes"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync("origin", "human:maya"); err != nil {
		t.Fatal(err)
	}

	entries, err := b.AuditLog("docs/spec.html", time.Time{}, "", 50)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}
	var actors []string
	for _, e := range entries {
		actors = append(actors, e.Actor)
	}
	joined := strings.Join(actors, ",")
	if !strings.Contains(joined, "human:josh") || !strings.Contains(joined, "agent:hermes") {
		t.Fatalf("audit log missing attribution: %v", actors)
	}
}

func TestOfflineRejoinCatchesUp(t *testing.T) {
	_, a, b, _, sb := twoNodeFixture(t)

	// B is "offline": A makes two pushes.
	for _, msg := range []string{"change 1", "change 2"} {
		da, _ := a.storeLoad(t, "spec")
		da.AddParagraph(msg)
		_ = a.save(t, da)
		if _, err := a.CommitAll("human:josh", msg); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Sync("origin", "human:josh"); err != nil {
			t.Fatal(err)
		}
	}
	// B rejoins: one sync call, zero manual steps.
	if _, err := b.Sync("origin", "human:maya"); err != nil {
		t.Fatalf("rejoin sync: %v", err)
	}
	d, err := sb.Load("spec")
	if err != nil {
		t.Fatal(err)
	}
	if d.Block(getBlock(t, sb, "spec", "change 1").ID) == nil ||
		d.Block(getBlock(t, sb, "spec", "change 2").ID) == nil {
		t.Fatal("rejoin did not catch up")
	}
}

// helpers to keep the fixtures above readable
func (r *Repo) storeLoad(t *testing.T, slug string) (*store.Doc, error) {
	t.Helper()
	s, err := store.OpenStore(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	return s.Load(slug)
}

func (r *Repo) save(t *testing.T, d *store.Doc) error {
	t.Helper()
	s, err := store.OpenStore(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	return s.Save(d, d.Audit.LastEditor)
}

var _ = os.Getenv

// TestIdleSyncReportsNoActivity: once both nodes are converged, a sync cycle
// must report nothing pushed/pulled — the console once logged
// "pushed=true" every 5 seconds on idle coordinators because an
// already-up-to-date push was mislabeled as activity.
func TestIdleSyncReportsNoActivity(t *testing.T) {
	_, a, b, sa, _ := twoNodeFixture(t)

	da, _ := sa.Load("spec")
	da.AddParagraph("settled change")
	if err := sa.Save(da, "human:josh"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitAll("human:josh", "settle"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Sync("origin", "human:josh"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync("origin", "human:maya"); err != nil {
		t.Fatal(err)
	}

	// both nodes fully converged: idle cycles must be quiet
	for i := 0; i < 3; i++ {
		res, err := a.Sync("origin", "human:josh")
		if err != nil {
			t.Fatalf("idle sync: %v", err)
		}
		if res.Pulled || res.Pushed || res.Merged {
			t.Fatalf("idle cycle reported activity: %+v", res)
		}
	}
}
