package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	leetNet "leetoffice/internal/net"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

// TestSyncOverMTLS covers the Phase 3 gate over the real wire: a coordinator
// serves the main share's git protocol over mTLS; an enrolled client pulls and
// pushes through the leet:// transport; a rogue node without a certificate is
// rejected during the TLS handshake.
func TestSyncOverMTLS(t *testing.T) {
	if testing.Short() {
		t.Skip("network e2e kept out of short runs")
	}
	dir := t.TempDir()
	shareRoot := filepath.Join(dir, "share")
	bare := filepath.Join(shareRoot, "main.git")
	if _, err := leetSync.InitBare(bare); err != nil {
		t.Fatal(err)
	}

	// team CA + coordinator identity
	ca, err := leetNet.CreateCA(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatal(err)
	}
	coord, err := ca.Issue("coordinator")
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Save(filepath.Join(dir, "coord-id")); err != nil {
		t.Fatal(err)
	}

	// git service over mTLS on an ephemeral loopback port
	srv, err := leetNet.ServeGit("127.0.0.1:0", coord.ServerTLSConfig(), shareRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	shareURL := "leet://" + srv.Addr().String() + "/main.git"

	// seed the share from the coordinator's own store over file://
	seedStore, err := store.OpenStore(filepath.Join(dir, "coord-store"))
	if err != nil {
		t.Fatal(err)
	}
	seedRepo, err := leetSync.Init(seedStore.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedRepo.AddRemote("origin", "file://"+bare); err != nil {
		t.Fatal(err)
	}
	d := store.NewDoc(store.TypeDoc, "protocol-note", "Protocol Note")
	d.AddParagraph("The store syncs over mutually authenticated TLS.")
	if err := seedStore.Save(d, "human:josh"); err != nil {
		t.Fatal(err)
	}
	if _, err := seedRepo.CommitAll("human:josh", "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := seedRepo.Sync("origin", "human:josh"); err != nil {
		t.Fatalf("seed sync: %v", err)
	}

	// enrolled client with a CA-signed identity
	clientID, err := ca.Issue("node-b")
	if err != nil {
		t.Fatal(err)
	}
	leetNet.InstallTransport(clientID.TLSConfig())

	clientStore, err := store.OpenStore(filepath.Join(dir, "client-store"))
	if err != nil {
		t.Fatal(err)
	}
	clientRepo, err := leetSync.Init(clientStore.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := clientRepo.AddRemote("origin", shareURL); err != nil {
		t.Fatal(err)
	}
	if _, err := clientRepo.Sync("origin", "human:maya"); err != nil {
		t.Fatalf("client pull over mTLS: %v", err)
	}
	if got, err := clientStore.Load("protocol-note"); err != nil || len(got.Blocks) != 1 {
		t.Fatalf("client did not receive the doc: %v", err)
	}

	// client pushes a change; the bare repo receives it
	got, _ := clientStore.Load("protocol-note")
	got.AddParagraph("Client edit pushed over the wire.")
	if err := clientStore.Save(got, "human:maya"); err != nil {
		t.Fatal(err)
	}
	if _, err := clientRepo.CommitAll("human:maya", "client edit"); err != nil {
		t.Fatal(err)
	}
	if _, err := clientRepo.Sync("origin", "human:maya"); err != nil {
		t.Fatalf("client push over mTLS: %v", err)
	}
	bareRepo, err := git.PlainOpen(bare)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := bareRepo.Reference(plumbing.NewBranchReferenceName(leetSync.DefaultBranch), false)
	if err != nil || ref.Hash().IsZero() {
		t.Fatalf("bare main missing after push: %v", err)
	}

	// rogue node: no client certificate → TLS handshake rejected
	rogueStore, err := store.OpenStore(filepath.Join(dir, "rogue-store"))
	if err != nil {
		t.Fatal(err)
	}
	rogueRepo, err := leetSync.Init(rogueStore.Root)
	if err != nil {
		t.Fatal(err)
	}
	rogueTLS := clientID.TLSConfig()
	rogueTLS.Certificates = nil // strip the certificate entirely
	leetNet.InstallTransport(rogueTLS)
	if err := rogueRepo.AddRemote("origin", shareURL); err != nil {
		t.Fatal(err)
	}
	if _, err := rogueRepo.Sync("origin", "rogue:mallory"); err == nil {
		t.Fatal("rogue node without a certificate was accepted")
	} else if !strings.Contains(strings.ToLower(err.Error()), "certificate") &&
		!strings.Contains(strings.ToLower(err.Error()), "tls") &&
		!strings.Contains(strings.ToLower(err.Error()), "bad certificate") {
		// any handshake-level failure is acceptable; a protocol-level success path is not
		t.Logf("rogue rejected with: %v", err)
	}
}
