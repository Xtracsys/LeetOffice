package net

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// Node files persisted under a node dir (BUILD_SPEC §6.3 step 3).
const (
	caKeyFile  = "ca.key"
	caCertFile = "ca.crt"
	nodeKey    = "node.key"
	nodeCert   = "node.crt"
	certLife   = 10 * 365 * 24 * time.Hour // node/CA cert validity
)

// Fingerprint returns the hex-encoded SHA-256 of a certificate's DER
// encoding — the value pinned out-of-band and published in mDNS TXT
// records (§6.2, §6.3).
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// CA is the local team certificate authority (§6.3). The coordinator holds
// it and signs every node's mTLS certificate; member nodes only ever see
// ca.crt. Created once (CreateCA) and re-opened on restart (OpenCA).
type CA struct {
	priv    ed25519.PrivateKey
	cert    *x509.Certificate
	certPEM []byte
}

// CreateCA generates the team CA under dir (ca.key, ca.crt) and returns it.
// An existing CA is not overwritten; open it with OpenCA instead.
func CreateCA(dir string) (*CA, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "LeetOffice CA", Organization: []string{"LeetOffice"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(certLife),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		SubjectKeyId:          keyID(pub),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, caKeyFile), pem.EncodeToMemory(
		&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, caCertFile), pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return nil, err
	}
	return newCA(priv, der)
}

// OpenCA loads the team CA previously created under dir.
func OpenCA(dir string) (*CA, error) {
	keyPEM, err := os.ReadFile(filepath.Join(dir, caKeyFile))
	if err != nil {
		return nil, err
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, caCertFile))
	if err != nil {
		return nil, err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" {
		return nil, errors.New("net: ca.key is not a PEM private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("net: ca.key is not an ed25519 key")
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, errors.New("net: ca.crt is not a PEM certificate")
	}
	return newCA(priv, certBlock.Bytes)
}

func newCA(priv ed25519.PrivateKey, certDER []byte) (*CA, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}
	return &CA{priv: priv, cert: cert,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})}, nil
}

// Fingerprint returns Fingerprint of the CA certificate — the team-wide pin
// a joining node verifies during enrollment (§6.3).
func (c *CA) Fingerprint() string { return Fingerprint(c.cert) }

// CertPEM returns the CA certificate in PEM form (written alongside issued
// node certificates so members can verify peers).
func (c *CA) CertPEM() []byte { return c.certPEM }

// Issue generates a fresh keypair for nodeID and returns its identity,
// signed directly by the CA. This is how the coordinator creates its own
// identity and how tests mint nodes without the enrollment round-trip.
func (c *CA) Issue(nodeID string) (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := c.sign(pub, pkix.Name{CommonName: nodeID, Organization: []string{"LeetOffice"}}, []string{nodeID})
	if err != nil {
		return nil, err
	}
	return newIdentity(priv, der, c.cert.Raw)
}

// SignCSR issues a node certificate for an enrollment CSR (§6.3 step 2).
// The node id is the CSR's first DNS SAN (falling back to its subject CN).
func (c *CA) SignCSR(csrPEM []byte) (certPEM []byte, err error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, errors.New("net: enrollment payload is not a CSR")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("net: CSR signature: %w", err)
	}
	dns := csr.DNSNames
	if len(dns) == 0 && csr.Subject.CommonName != "" {
		dns = []string{csr.Subject.CommonName}
	}
	if len(dns) == 0 {
		return nil, errors.New("net: CSR carries no node id (SAN or CN)")
	}
	der, err := c.sign(csr.PublicKey, csr.Subject, dns)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// sign produces a leaf certificate binding pub to the SAN node id(s).
func (c *CA) sign(pub crypto.PublicKey, subject pkix.Name, dns []string) ([]byte, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      subject,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(certLife),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		// Nodes are peers: any of them may act as client or server (D4).
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		DNSNames:    dns,
	}
	return x509.CreateCertificate(rand.Reader, tmpl, c.cert, pub, c.priv)
}

// Identity is a node's mTLS identity: an ed25519 keypair plus a certificate
// signed by the team CA, with the node id as DNS SAN (§6.3 step 3).
type Identity struct {
	priv ed25519.PrivateKey
	cert *x509.Certificate
	ca   *x509.Certificate
	leaf tls.Certificate
	pool *x509.CertPool
}

func newIdentity(priv ed25519.PrivateKey, certDER, caDER []byte) (*Identity, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return &Identity{
		priv: priv,
		cert: cert,
		ca:   ca,
		leaf: tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: priv, Leaf: cert},
		pool: pool,
	}, nil
}

// NodeID returns the node id from the certificate SAN (or CN as fallback).
func (id *Identity) NodeID() string {
	if len(id.cert.DNSNames) > 0 {
		return id.cert.DNSNames[0]
	}
	return id.cert.Subject.CommonName
}

// Fingerprint returns the pin of this node's certificate (mDNS TXT "fp").
func (id *Identity) Fingerprint() string { return Fingerprint(id.cert) }

// Save persists the identity under dir (node.key, node.crt, ca.crt).
func (id *Identity) Save(dir string) error {
	keyDER, err := x509.MarshalPKCS8PrivateKey(id.priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, nodeKey), pem.EncodeToMemory(
		&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, nodeCert), pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: id.cert.Raw}), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, caCertFile), pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: id.ca.Raw}), 0o644)
}

// LoadIdentity reloads an identity previously written by Save.
func LoadIdentity(dir string) (*Identity, error) {
	keyPEM, err := os.ReadFile(filepath.Join(dir, nodeKey))
	if err != nil {
		return nil, err
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, nodeCert))
	if err != nil {
		return nil, err
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, caCertFile))
	if err != nil {
		return nil, err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" {
		return nil, errors.New("net: node.key is not a PEM private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("net: node.key is not an ed25519 key")
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, errors.New("net: node.crt is not a PEM certificate")
	}
	caBlock, _ := pem.Decode(caPEM)
	if caBlock == nil || caBlock.Type != "CERTIFICATE" {
		return nil, errors.New("net: ca.crt is not a PEM certificate")
	}
	return newIdentity(priv, certBlock.Bytes, caBlock.Bytes)
}

// TLSConfig is the client side of mTLS: present our certificate and accept
// only peers whose chain roots at the team CA. Git-style hostname matching
// is deliberately replaced by membership verification (D8) — any CA-signed
// node is a legitimate peer, the SAN carries who it is.
func (id *Identity) TLSConfig() *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{id.leaf},
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // hostname check replaced by verifyPeer
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return id.verifyPeer(rawCerts)
		},
	}
}

// ServerTLSConfig is the serving side: client certificates are REQUIRED and
// must chain to the team CA — this is the handshake-time rogue-node gate.
func (id *Identity) ServerTLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{id.leaf},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    id.pool,
		MinVersion:   tls.VersionTLS13,
	}
}

// EnrollmentTLSConfig serves the enrollment endpoint: the coordinator
// presents its certificate but does not require a client certificate —
// joining nodes have none yet, the one-time secret is the gate (§6.3).
func (id *Identity) EnrollmentTLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{id.leaf},
		ClientAuth:   tls.NoClientCert,
		ClientCAs:    id.pool,
		MinVersion:   tls.VersionTLS13,
	}
}

// verifyPeer accepts only certificates issued by the team CA.
func (id *Identity) verifyPeer(rawCerts [][]byte) error {
	for _, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return err
		}
		if _, err := cert.Verify(x509.VerifyOptions{
			Roots: id.pool,
			KeyUsages: []x509.ExtKeyUsage{
				x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth,
			},
		}); err != nil {
			return fmt.Errorf("net: peer not a team member: %w", err)
		}
	}
	return nil
}

// randomSerial returns a fresh random certificate serial number.
func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 127)
	return rand.Int(rand.Reader, limit)
}

// keyID derives an X.509 SubjectKeyId from a public key.
func keyID(pub crypto.PublicKey) []byte {
	sum := sha256.Sum256(pub.(ed25519.PublicKey))
	return sum[16:]
}
