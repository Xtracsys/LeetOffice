package sync

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"leetoffice/internal/store"
)

// Regression for FR-031: Store.Save writes files WITHOUT the repo lock, and
// Sync's destructive worktree operations (fast-forward hard reset, merge
// file writes) used to silently discard in-flight saves — observed at 248
// of 250 writes lost in a stress probe before the fix. The fix snapshots
// in-flight edits under the repo lock before any destructive operation,
// turning them into commits the block-merge preserves.
//
// The writer APPENDS unique markers; after quiescing, every marker must
// still be in the doc (a vanishing marker is a lost write). A tiny residual
// window remains between a dirty-check and a destructive write inside the
// lock — losses above 5% fail; the complete fix is the write-transaction
// refactor (proposed FR-032). Skipped in -short: the loops run for seconds.
func TestSyncDoesNotDiscardInFlightSaves(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-sensitive stress probe")
	}
	dir := t.TempDir()
	bare := filepath.Join(t.TempDir(), "main.git")
	if _, err := InitBare(bare); err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Init(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddRemote("origin", "file://"+bare); err != nil {
		t.Fatal(err)
	}

	d := store.NewDoc(store.TypeDoc, "probe", "Probe")
	d.AddParagraph("seed")
	if err := s.Save(d, "human:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitAll("human:a", "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sync("origin", "human:a"); err != nil {
		t.Fatal(err)
	}

	// Peer repo keeps the remote ahead (fast-forward territory for A).
	pdir := filepath.Join(dir, "peer")
	ps, err := store.OpenStore(pdir)
	if err != nil {
		t.Fatal(err)
	}
	pr, err := Init(pdir)
	if err != nil {
		t.Fatal(err)
	}
	if err := pr.AddRemote("origin", "file://"+bare); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Sync("origin", "human:peer"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() { // peer keeps pushing
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			pd, err := ps.Load("peer-doc")
			if err != nil {
				pd = store.NewDoc(store.TypeDoc, "peer-doc", "Peer")
			}
			pd.AddParagraph(fmt.Sprintf("peer-%d", i))
			_ = ps.Save(pd, "human:peer")
			_, _ = pr.CommitAll("human:peer", "peer write")
			_, _ = pr.Sync("origin", "human:peer")
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Add(1)
	go func() { // A syncs constantly
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = r.Sync("origin", "human:a")
			time.Sleep(time.Millisecond)
		}
	}()

	const iters = 60
	markers := make([]string, iters)
	for i := 0; i < iters; i++ {
		markers[i] = fmt.Sprintf("write-%04d", i)
		dd, err := s.Load("probe")
		if err != nil {
			t.Fatal(err)
		}
		dd.AddParagraph(markers[i])
		if err := s.Save(dd, "human:a"); err != nil {
			t.Fatal(err)
		}
		if _, cerr := r.CommitAll("human:a", "writer write"); errors.Is(cerr, ErrNoChanges) {
			t.Logf("iteration %d: commit reported no changes (in-flight write raced)", i)
		} else if cerr != nil {
			t.Fatal(cerr)
		}
	}
	close(stop)
	wg.Wait()

	// The final write must survive deterministically.
	dd, _ := s.Load("probe")
	dd.AddParagraph("final")
	if err := s.Save(dd, "human:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitAll("human:a", "final"); errors.Is(err, ErrNoChanges) {
		t.Fatal("final write was discarded")
	}
	if _, err := r.Sync("origin", "human:a"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Load("probe")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for _, b := range got.Blocks {
		body.WriteString(b.Content)
		body.WriteString("\n")
	}
	if !strings.Contains(body.String(), "final") {
		t.Fatal("final marker missing after sync")
	}
	missing := 0
	for _, m := range markers {
		if !strings.Contains(body.String(), m) {
			missing++
		}
	}
	if missing > iters/20 { // >5% lost = regression to the pre-fix behavior
		t.Fatalf("%d of %d in-flight writes were lost during sync", missing, iters)
	}
	if missing > 0 {
		t.Logf("%d of %d writes hit the documented residual window", missing, iters)
	}
}
