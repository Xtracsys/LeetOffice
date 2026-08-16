package net

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"leetoffice/internal/store"
	leetsync "leetoffice/internal/sync"
)

// shareFixture builds a coordinator serving bare repos under a temp
// "shares" root over mTLS (ServeGit), plus a client identity enrolled
// through the enrollment server — the full §6.3 + §6.4 path.
func shareFixture(t *testing.T) (dir, shares string, srv *GitServer, client *Identity) {
	t.Helper()
	dir = t.TempDir()
	shares = filepath.Join(dir, "shares")
	ca, coord := teamFixture(t, dir)
	srv, err := ServeGit("127.0.0.1:0", coord.ServerTLSConfig(), shares)
	if err != nil {
		t.Fatalf("ServeGit: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	const secret = "one-time-team-secret"
	es, err := NewEnrollmentServer(ca, secret, "127.0.0.1:0", coord.EnrollmentTLSConfig(), 7418)
	if err != nil {
		t.Fatalf("NewEnrollmentServer: %v", err)
	}
	t.Cleanup(func() { _ = es.Close() })
	client, gitAddr, err := Enroll(es.Addr().String(), "node-b", secret, ca.Fingerprint())
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	// the joiner must learn the GIT service address, not the enrollment
	// port it just dialed — the two-port mixup that orphaned joined nodes
	if gitAddr != "127.0.0.1:7418" {
		t.Fatalf("coordinator advertised git addr %q, want 127.0.0.1:7418 (enrollment host + git port, NOT the enrollment port)", gitAddr)
	}
	return dir, shares, srv, client
}

// newNode creates a store + git repo whose "origin" points at remoteURL.
func newNode(t *testing.T, root, remoteURL string) (*store.Store, *leetsync.Repo) {
	t.Helper()
	s, err := store.OpenStore(root)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	r, err := leetsync.Init(root)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := r.AddRemote("origin", remoteURL); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	return s, r
}

func leetURL(srv *GitServer, repo string) string {
	return fmt.Sprintf("%s://%s/%s", Scheme, srv.Addr().String(), repo)
}

func containsBlock(d *store.Doc, content string) bool {
	for _, b := range d.Blocks {
		if b.Content == content {
			return true
		}
	}
	return false
}

// blobAt reads a file out of a bare repo's HEAD commit tree.
func blobAt(t *testing.T, repo *gogit.Repository, refName plumbing.ReferenceName, path string) string {
	t.Helper()
	ref, err := repo.Reference(refName, true)
	if err != nil {
		t.Fatalf("reference %s: %v", refName, err)
	}
	commit, err := object.GetCommit(repo.Storer, ref.Hash())
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	f, err := commit.File(path)
	if err != nil {
		t.Fatalf("file %s at %s: %v", path, ref.Hash(), err)
	}
	rc, err := f.Blob.Reader()
	if err != nil {
		t.Fatalf("blob reader: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	return string(data)
}

func TestSyncOverLeet(t *testing.T) {
	dir, shares, srv, client := shareFixture(t)
	InstallTransport(client.TLSConfig())

	// Seed the main share from node A over file:// (as in leet-sync's
	// two-node fixture), then serve the same bare repo over leet://.
	bare := filepath.Join(shares, "main.git")
	if _, err := leetsync.InitBare(bare); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	nodeA := filepath.Join(dir, "node-a")
	sa, a := newNode(t, nodeA, "file://"+bare)
	seed := store.NewDoc(store.TypeDoc, "spec", "Spec Doc")
	seed.AddParagraph("one")
	seed.AddParagraph("two")
	if err := sa.Save(seed, "human:josh"); err != nil {
		t.Fatalf("save seed: %v", err)
	}
	if _, err := a.CommitAll("human:josh", "seed store"); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	ga, err := gogit.PlainOpen(nodeA)
	if err != nil {
		t.Fatalf("open node-a: %v", err)
	}
	if err := ga.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"+refs/heads/main:refs/heads/main"},
	}); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	// Node B enrolls (done in the fixture) and pulls via leet://.
	nodeB := filepath.Join(dir, "node-b")
	sb, b := newNode(t, nodeB, leetURL(srv, "main.git"))
	res, err := b.Sync("origin", "human:maya")
	if err != nil {
		t.Fatalf("node-b initial sync over leet: %v", err)
	}
	if !res.Pulled {
		t.Fatalf("expected a pull, got %+v", res)
	}
	pulled, err := sb.Load("spec")
	if err != nil {
		t.Fatalf("load after pull: %v", err)
	}
	if !containsBlock(pulled, "one") || !containsBlock(pulled, "two") {
		t.Fatalf("pull missed seeded blocks: %+v", pulled.Blocks)
	}

	// Node B commits and pushes over leet://.
	pulled.AddParagraph("three from node-b")
	if err := sb.Save(pulled, "human:maya"); err != nil {
		t.Fatalf("save node-b edit: %v", err)
	}
	if _, err := b.CommitAll("human:maya", "node-b adds three"); err != nil {
		t.Fatalf("commit node-b: %v", err)
	}
	res, err = b.Sync("origin", "human:maya")
	if err != nil {
		t.Fatalf("node-b push over leet: %v", err)
	}
	if !res.Pushed {
		t.Fatalf("expected a push, got %+v", res)
	}

	// The bare main share received the commit and its content.
	gb, err := gogit.PlainOpen(bare)
	if err != nil {
		t.Fatalf("reopen bare: %v", err)
	}
	main := plumbing.NewBranchReferenceName("main")
	gbHead, err := gb.Reference(main, true)
	if err != nil {
		t.Fatalf("bare main ref: %v", err)
	}
	gB, err := gogit.PlainOpen(nodeB)
	if err != nil {
		t.Fatalf("reopen node-b: %v", err)
	}
	localHead, err := gB.Reference(main, true)
	if err != nil {
		t.Fatalf("node-b main ref: %v", err)
	}
	if gbHead.Hash() != localHead.Hash() {
		t.Fatalf("bare main = %s, node-b main = %s", gbHead.Hash(), localHead.Hash())
	}
	if page := blobAt(t, gb, main, "docs/spec.html"); !strings.Contains(page, "three from node-b") {
		t.Fatal("pushed content missing from the bare main share")
	}
}

func TestPushFirstCommitToEmptyShareOverLeet(t *testing.T) {
	dir, shares, srv, client := shareFixture(t)
	InstallTransport(client.TLSConfig())

	// A brand-new bare share advertises nothing; the first push must
	// still work (empty-advrefs receive-pack path).
	bare := filepath.Join(shares, "fresh.git")
	if _, err := leetsync.InitBare(bare); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	nodeC := filepath.Join(dir, "node-c")
	sc, c := newNode(t, nodeC, leetURL(srv, "fresh.git"))
	doc := store.NewDoc(store.TypeTask, "first-task", "First Task")
	doc.AddParagraph("kickoff")
	if err := sc.Save(doc, "agent:hermes"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := c.CommitAll("agent:hermes", "first commit"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	gc, err := gogit.PlainOpen(nodeC)
	if err != nil {
		t.Fatalf("open node-c: %v", err)
	}
	if err := gc.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"+refs/heads/main:refs/heads/main"},
	}); err != nil {
		t.Fatalf("first push to empty share: %v", err)
	}

	gb, err := gogit.PlainOpen(bare)
	if err != nil {
		t.Fatalf("reopen bare: %v", err)
	}
	main := plumbing.NewBranchReferenceName("main")
	if page := blobAt(t, gb, main, "tasks/first-task.html"); !strings.Contains(page, "kickoff") {
		t.Fatal("first push content missing from the bare share")
	}
}

func TestFetchMissingRepoOverLeet(t *testing.T) {
	dir, _, srv, client := shareFixture(t)
	InstallTransport(client.TLSConfig())

	remote := leetURL(srv, "nope.git")
	root := filepath.Join(dir, "node-missing")
	newNode(t, root, remote)
	gr, err := gogit.PlainOpen(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	err = gr.Fetch(&gogit.FetchOptions{RemoteName: "origin", Force: true})
	if !errors.Is(err, transport.ErrRepositoryNotFound) {
		t.Fatalf("expected ErrRepositoryNotFound, got %v", err)
	}
}

func TestRogueNodeRejectedByGitServer(t *testing.T) {
	dir, _, srv, _ := shareFixture(t)

	// A node from a foreign CA (or with no cert) cannot even handshake
	// with the git server — its sync traffic is rejected unread.
	rogueCA, err := CreateCA(filepath.Join(dir, "rogue-ca"))
	if err != nil {
		t.Fatalf("rogue CA: %v", err)
	}
	rogue, err := rogueCA.Issue("rogue")
	if err != nil {
		t.Fatalf("rogue identity: %v", err)
	}
	c, err := tls.Dial("tcp", srv.Addr().String(), rogue.TLSConfig())
	if err == nil {
		c.Close()
		t.Fatal("rogue node completed a handshake with the git server")
	}
}
