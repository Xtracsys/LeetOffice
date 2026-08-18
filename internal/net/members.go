package net

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// IssuedDir is the coordinator folder of certificates handed out at enroll.
const IssuedDir = "issued"

// IssuedMember is one node the team CA has certified.
type IssuedMember struct {
	NodeID      string
	Issued      time.Time
	NotAfter    time.Time
	Fingerprint string
}

// RecordIssued writes certPEM as <dir>/<nodeID>.crt so Settings can list
// who has been accepted. Best-effort: a write failure does not fail enroll.
func RecordIssued(dir, nodeID string, certPEM []byte) error {
	if dir == "" || nodeID == "" || len(certPEM) == 0 {
		return nil
	}
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '-'
		}
		return r
	}, nodeID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, safe+".crt"), certPEM, 0o644)
}

// ListIssued returns accepted members from dir, newest first.
func ListIssued(dir string) []IssuedMember {
	if dir == "" {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []IssuedMember
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".crt") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		id := cert.Subject.CommonName
		if id == "" && len(cert.DNSNames) > 0 {
			id = cert.DNSNames[0]
		}
		if id == "" {
			id = strings.TrimSuffix(e.Name(), ".crt")
		}
		out = append(out, IssuedMember{
			NodeID:      id,
			Issued:      cert.NotBefore,
			NotAfter:    cert.NotAfter,
			Fingerprint: Fingerprint(cert),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Issued.After(out[j].Issued) })
	return out
}
