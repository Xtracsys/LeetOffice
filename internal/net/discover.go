package net

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

// ServiceName is the mDNS service type every LeetOffice node announces
// (§6.2), so clients resolve the coordinator without configuration.
const ServiceName = "_leetoffice._tcp"

// Peer is a discovered node: where its leet:// sync endpoint lives, its
// role, and the fingerprint with which to pin its certificate.
type Peer struct {
	NodeID      string
	Addr        string // host:git-port (the leet:// sync endpoint)
	Role        string
	Fingerprint string
	EnrollPort  int // where a joiner enrolls (coordinators only)
}

// peerTXT assembles the mDNS TXT records for an announcement: node_id,
// role, and the certificate fingerprint (§6.2).
func peerTXT(nodeID, role, fingerprint string, enrollPort int) []string {
	fields := []string{
		"node_id=" + nodeID,
		"role=" + role,
		"fp=" + fingerprint,
	}
	if enrollPort > 0 {
		fields = append(fields, "enroll="+strconv.Itoa(enrollPort))
	}
	return fields
}

// parsePeerTXT extracts the LeetOffice TXT fields. An empty nodeID means
// the records did not come from a LeetOffice node.
func parsePeerTXT(fields []string) (nodeID, role, fingerprint string, enrollPort int) {
	for _, f := range fields {
		switch {
		case strings.HasPrefix(f, "node_id="):
			nodeID = strings.TrimPrefix(f, "node_id=")
		case strings.HasPrefix(f, "role="):
			role = strings.TrimPrefix(f, "role=")
		case strings.HasPrefix(f, "fp="):
			fingerprint = strings.TrimPrefix(f, "fp=")
		case strings.HasPrefix(f, "enroll="):
			enrollPort, _ = strconv.Atoi(strings.TrimPrefix(f, "enroll="))
		}
	}
	return nodeID, role, fingerprint, enrollPort
}

// Announcer broadcasts this node over mDNS until Shutdown.
type Announcer struct {
	srv *mdns.Server
}

// Announce starts announcing _leetoffice._tcp with the node's TXT records.
// host defaults to the machine hostname; port is the node's leet:// sync
// port (GitServer.Addr's port).
func Announce(nodeID, role, fingerprint, host string, port, enrollPort int) (*Announcer, error) {
	svc, err := mdns.NewMDNSService(nodeID, ServiceName, "", host, port, nil,
		peerTXT(nodeID, role, fingerprint, enrollPort))
	if err != nil {
		return nil, err
	}
	srv, err := mdns.NewServer(&mdns.Config{Zone: svc})
	if err != nil {
		return nil, err
	}
	return &Announcer{srv: srv}, nil
}

// Shutdown stops the announcement.
func (a *Announcer) Shutdown() error { return a.srv.Shutdown() }

// DiscoverPeers resolves _leetoffice._tcp on the LAN, collecting answers
// for up to timeout. Entries without LeetOffice TXT records are ignored.
func DiscoverPeers(timeout time.Duration) ([]Peer, error) {
	entries := make(chan *mdns.ServiceEntry, 16)
	params := mdns.DefaultParams(ServiceName)
	params.Timeout = timeout
	params.Entries = entries
	errc := make(chan error, 1)
	go func() { errc <- mdns.Query(params) }()

	var peers []Peer
	for {
		select {
		case e := <-entries:
			if p, ok := peerFromEntry(e); ok {
				peers = append(peers, p)
			}
		case err := <-errc:
			for {
				select {
				case e := <-entries:
					if p, ok := peerFromEntry(e); ok {
						peers = append(peers, p)
					}
					continue
				default:
				}
				break
			}
			return peers, err
		}
	}
}

func peerFromEntry(e *mdns.ServiceEntry) (Peer, bool) {
	nodeID, role, fp, enrollPort := parsePeerTXT(e.InfoFields)
	if nodeID == "" {
		return Peer{}, false
	}
	ip := e.AddrV4
	if ip == nil {
		ip = e.Addr
	}
	if ip == nil {
		return Peer{}, false
	}
	return Peer{
		NodeID:      nodeID,
		Addr:        net.JoinHostPort(ip.String(), strconv.Itoa(e.Port)),
		Role:        role,
		Fingerprint: fp,
		EnrollPort:  enrollPort,
	}, true
}
