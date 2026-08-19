package net

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestPeerTXTAssemblyAndParse(t *testing.T) {
	fields := peerTXT("node-a", "coordinator", "ab12cd34", 7443)
	want := []string{"node_id=node-a", "role=coordinator", "fp=ab12cd34", "enroll=7443"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("TXT records = %v, want %v", fields, want)
	}
	nodeID, role, fp, ep := parsePeerTXT(fields)
	if nodeID != "node-a" || role != "coordinator" || fp != "ab12cd34" || ep != 7443 {
		t.Fatalf("parsed (%q,%q,%q,enroll=%d), want (node-a,coordinator,ab12cd34,7443)", nodeID, role, fp, ep)
	}

	// Order must not matter and unknown records must be ignored.
	shuffled := []string{"someother=x", "fp=ab12cd34", "node_id=node-a", "role=coordinator"}
	if nodeID, role, fp, _ = parsePeerTXT(shuffled); nodeID != "node-a" || role != "coordinator" || fp != "ab12cd34" {
		t.Fatalf("shuffled parse = (%q,%q,%q)", nodeID, role, fp)
	}

	// Non-LeetOffice mDNS records are not peers.
	if nodeID, _, _, _ = parsePeerTXT([]string{"path=/srv"}); nodeID != "" {
		t.Fatalf("foreign record parsed as peer %q", nodeID)
	}
}

func TestDiscoverPeersHonorsTimeout(t *testing.T) {
	start := time.Now()
	_, err := DiscoverPeers(150 * time.Millisecond)
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("DiscoverPeers took %s (err=%v) — timeout must bound Settings/API", elapsed, err)
	}
}

func TestDiscoverPeersOverlappingLookupsReturn(t *testing.T) {
	start := time.Now()
	errc := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := DiscoverPeers(150 * time.Millisecond)
			errc <- err
		}()
	}
	for i := 0; i < 4; i++ {
		<-errc
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("overlapping DiscoverPeers took %s — QueryContext must cancel", time.Since(start))
	}
}

func TestAnnounceRejectsZeroPort(t *testing.T) {
	_, err := Announce("node", "client", "", "", 0, 0)
	if err == nil {
		t.Fatal("Announce(port=0) succeeded; hashicorp/mdns must reject it")
	}
}

func TestAnnouncePresencePort(t *testing.T) {
	a, err := Announce("leet-presence-node", "client", "", "", PresencePort, 0)
	if err != nil {
		t.Fatalf("Announce(PresencePort): %v", err)
	}
	if err := a.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestAnnounceAndDiscoverOverMulticast(t *testing.T) {
	if testing.Short() {
		t.Skip("real mDNS multicast kept out of short/CI runs")
	}
	if err := probeMulticast(); err != nil {
		t.Skipf("no mDNS multicast route in this environment: %v", err)
	}
	a, err := Announce("leet-test-node", "coordinator", "test-fp", "", 7418, 7443)
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	defer a.Shutdown()

	peers, err := DiscoverPeers(3 * time.Second)
	if err != nil {
		t.Fatalf("DiscoverPeers: %v", err)
	}
	for _, p := range peers {
		if p.NodeID == "leet-test-node" {
			if p.Role != "coordinator" || p.Fingerprint != "test-fp" {
				t.Fatalf("discovered peer %+v lost TXT fields", p)
			}
			return
		}
	}
	t.Fatalf("did not discover our own announcement in %d peers", len(peers))
}

// probeMulticast reports whether this environment can reach the mDNS
// group at all (sandboxes and container CI often cannot).
func probeMulticast() error {
	// send a probe and verify we can receive multicast at all — a bare write
	// succeeds even on hosts where multicast delivery is black-holed.
	c, err := net.Dial("udp4", "224.0.0.251:5353")
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := c.Write([]byte("leetprobe")); err != nil {
		return err
	}
	lo, err := net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
	if err != nil {
		return err
	}
	defer lo.Close()
	if err := lo.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		return err
	}
	buf := make([]byte, 64)
	for {
		n, _, err := lo.ReadFromUDP(buf)
		if err != nil {
			return fmt.Errorf("no multicast traffic received: %w", err)
		}
		if bytes.Contains(buf[:n], []byte("leetprobe")) {
			return nil
		}
	}
}
