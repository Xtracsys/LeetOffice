package net

import (
	"crypto/tls"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// teamFixture creates the trust setup of §6.3: a team CA plus the
// coordinator's identity.
func teamFixture(t *testing.T, dir string) (*CA, *Identity) {
	t.Helper()
	ca, err := CreateCA(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatalf("CreateCA: %v", err)
	}
	coord, err := ca.Issue("coordinator")
	if err != nil {
		t.Fatalf("Issue coordinator: %v", err)
	}
	return ca, coord
}

func TestCAIssueAndIdentityRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ca, _ := teamFixture(t, dir)

	id, err := ca.Issue("node-a")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if id.NodeID() != "node-a" {
		t.Fatalf("node id = %q, want node-a", id.NodeID())
	}
	if len(id.Fingerprint()) != 64 {
		t.Fatalf("fingerprint %q is not 64 hex chars", id.Fingerprint())
	}
	// The identity must carry the CA's SAN discipline: node id as DNS SAN.
	if got := id.cert.DNSNames; len(got) != 1 || got[0] != "node-a" {
		t.Fatalf("cert SANs = %v, want [node-a]", got)
	}

	// Persist + reload.
	nodeDir := filepath.Join(dir, "node-a")
	if err := id.Save(nodeDir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadIdentity(nodeDir)
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	if loaded.NodeID() != id.NodeID() || loaded.Fingerprint() != id.Fingerprint() {
		t.Fatal("reloaded identity does not match")
	}

	// Reopening the CA from disk issues certificates for the same team.
	ca2, err := OpenCA(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatalf("OpenCA: %v", err)
	}
	if ca2.Fingerprint() != ca.Fingerprint() {
		t.Fatal("reopened CA fingerprint changed")
	}
}

func TestMTLSMembersHandshakeRoguesRejected(t *testing.T) {
	dir := t.TempDir()
	ca, server := teamFixture(t, dir)
	member, err := ca.Issue("node-a")
	if err != nil {
		t.Fatalf("issue member: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go func(raw net.Conn) {
				defer raw.Close()
				conn := tls.Server(raw, server.ServerTLSConfig())
				_ = conn.Handshake() // rogues fail here
			}(raw)
		}
	}()

	// A CA-issued member completes the handshake and sees the server's SAN.
	c, err := tls.Dial("tcp", ln.Addr().String(), member.TLSConfig())
	if err != nil {
		t.Fatalf("member handshake rejected: %v", err)
	}
	state := c.ConnectionState()
	if len(state.PeerCertificates) == 0 || state.PeerCertificates[0].DNSNames[0] != "coordinator" {
		t.Fatal("member did not verify the coordinator certificate")
	}
	c.Close()

	// A rogue with its own (foreign-CA) certificate: rejected in-handshake
	// (with TLS 1.3 the rejection may only surface on the first read).
	rogueCA, err := CreateCA(filepath.Join(dir, "rogue"))
	if err != nil {
		t.Fatalf("rogue CA: %v", err)
	}
	rogue, err := rogueCA.Issue("rogue")
	if err != nil {
		t.Fatalf("rogue identity: %v", err)
	}
	if !rejectedByServer(t, ln.Addr().String(), rogue.TLSConfig()) {
		t.Fatal("rogue node was accepted by the mTLS server")
	}

	// A rogue with no certificate at all: rejected in-handshake.
	if !rejectedByServer(t, ln.Addr().String(),
		&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}) {
		t.Fatal("uncertificated client was accepted by the mTLS server")
	}
}

// rejectedByServer reports whether a connection with tlsConf fails the
// handshake or the first application-data read.
func rejectedByServer(t *testing.T, addr string, tlsConf *tls.Config) bool {
	t.Helper()
	c, err := tls.Dial("tcp", addr, tlsConf)
	if err != nil {
		return true
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = c.Read(make([]byte, 1))
	return err != nil
}

func TestEnrollment(t *testing.T) {
	dir := t.TempDir()
	ca, coord := teamFixture(t, dir)
	const secret = "one-time-team-secret"

	es, err := NewEnrollmentServer(ca, secret, "127.0.0.1:0", coord.EnrollmentTLSConfig())
	if err != nil {
		t.Fatalf("NewEnrollmentServer: %v", err)
	}
	defer es.Close()

	t.Run("correct secret issues a team certificate", func(t *testing.T) {
		id, err := Enroll(es.Addr().String(), "node-b", secret, ca.Fingerprint())
		if err != nil {
			t.Fatalf("Enroll: %v", err)
		}
		if id.NodeID() != "node-b" {
			t.Fatalf("node id = %q, want node-b", id.NodeID())
		}
		// The enrolled node now verifies the coordinator under the same
		// team CA — it is a full mTLS member.
		if err := id.verifyPeer([][]byte{coord.cert.Raw}); err != nil {
			t.Fatalf("enrolled identity does not trust the team: %v", err)
		}
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		_, err := Enroll(es.Addr().String(), "node-c", "not-the-secret", ca.Fingerprint())
		if err == nil || !strings.Contains(err.Error(), "wrong enrollment secret") {
			t.Fatalf("wrong secret not rejected: %v", err)
		}
	})

	t.Run("wrong CA pin is rejected", func(t *testing.T) {
		_, err := Enroll(es.Addr().String(), "node-d", secret, "0f00d00f")
		if err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
			t.Fatalf("mismatched pin not rejected: %v", err)
		}
	})
}
