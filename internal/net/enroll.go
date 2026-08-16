package net

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Enrollment request/response bodies (JSON over TLS).
type enrollRequest struct {
	CSR    string `json:"csr"`
	Secret string `json:"secret"`
}

type enrollResponse struct {
	Cert string `json:"cert"`
	CA   string `json:"ca"`
}

// EnrollmentServer is the coordinator side of the trust gate (§6.3): it
// exchanges CA-signed node certificates for the one-time team enrollment
// secret. A wrong secret gets 403 and no certificate.
type EnrollmentServer struct {
	srv *http.Server
	ln  net.Listener
}

// NewEnrollmentServer starts listening for enrollment requests on addr
// (use port 0 for an ephemeral port), presenting the coordinator's
// certificate via tlsConf (Identity.EnrollmentTLSConfig).
func NewEnrollmentServer(ca *CA, secret, addr string, tlsConf *tls.Config) (*EnrollmentServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	es := &EnrollmentServer{ln: ln, srv: &http.Server{Handler: enrollHandler(ca, secret), TLSConfig: tlsConf}}
	go func() { _ = es.srv.ServeTLS(ln, "", "") }()
	return es, nil
}

// Addr reports the bound address.
func (s *EnrollmentServer) Addr() net.Addr { return s.ln.Addr() }

// Close stops the enrollment server.
func (s *EnrollmentServer) Close() error { return s.srv.Close() }

func enrollHandler(ca *CA, secret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/enroll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var req enrollRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		// The one-time secret is the membership gate (D8): compare in
		// constant time and never leak why it failed.
		if subtle.ConstantTimeCompare([]byte(req.Secret), []byte(secret)) != 1 {
			http.Error(w, "wrong enrollment secret", http.StatusForbidden)
			return
		}
		certPEM, err := ca.SignCSR([]byte(req.CSR))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(enrollResponse{
			Cert: string(certPEM),
			CA:   string(ca.CertPEM()),
		})
	})
	return mux
}

// Enroll joins a team (§6.3): generate a keypair, send its CSR plus the
// one-time secret to the coordinator over TLS, and return the resulting
// identity. The coordinator's certificate must chain to the CA it returns;
// when caFingerprint is non-empty it must also match exactly (out-of-band
// pin of the coordinator's CA). Trust-on-first-use otherwise.
func Enroll(addr, nodeID, secret, caFingerprint string) (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	csrPEM, err := newCSRPEM(priv, nodeID)
	if err != nil {
		return nil, err
	}
	// InsecureSkipVerify is compensated below: the response carries the
	// team CA, and the peer we actually talked to must chain to it.
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13},
	}}
	body, _ := json.Marshal(enrollRequest{CSR: string(csrPEM), Secret: secret})
	resp, err := client.Post("https://"+addr+"/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("net: enrollment connection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var msg strings.Builder
		msg.WriteString("net: enrollment rejected")
		if resp.StatusCode == http.StatusForbidden {
			msg.WriteString(": wrong enrollment secret")
		} else {
			fmt.Fprintf(&msg, " (HTTP %d)", resp.StatusCode)
		}
		return nil, errors.New(msg.String())
	}
	var out enrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	certBlock, _ := pem.Decode([]byte(out.Cert))
	caBlock, _ := pem.Decode([]byte(out.CA))
	if certBlock == nil || caBlock == nil {
		return nil, errors.New("net: enrollment response missing PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, err
	}
	if caFingerprint != "" && Fingerprint(caCert) != caFingerprint {
		return nil, errors.New("net: coordinator CA fingerprint mismatch")
	}
	if len(resp.TLS.PeerCertificates) == 0 {
		return nil, errors.New("net: no coordinator certificate presented")
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := resp.TLS.PeerCertificates[0].Verify(x509.VerifyOptions{
		Roots: pool,
		KeyUsages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth,
		},
	}); err != nil {
		return nil, fmt.Errorf("net: coordinator not signed by the returned CA: %w", err)
	}
	id, err := newIdentity(priv, certBlock.Bytes, caBlock.Bytes)
	if err != nil {
		return nil, err
	}
	// The certificate must bind the key we just generated.
	if !pubEqual(id.cert.PublicKey, pub) {
		return nil, errors.New("net: issued certificate does not match our key")
	}
	if id.NodeID() != nodeID {
		return nil, fmt.Errorf("net: issued certificate has node id %q, want %q", id.NodeID(), nodeID)
	}
	return id, nil
}

// newCSRPEM builds the enrollment CSR for nodeID (SAN = node id).
func newCSRPEM(priv ed25519.PrivateKey, nodeID string) ([]byte, error) {
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: nodeID, Organization: []string{"LeetOffice"}},
		DNSNames:           []string{nodeID},
		SignatureAlgorithm: x509.PureEd25519,
	}, priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

// pubEqual compares two ed25519 public keys.
func pubEqual(a, b any) bool {
	ka, ok1 := a.(ed25519.PublicKey)
	kb, ok2 := b.(ed25519.PublicKey)
	return ok1 && ok2 && bytes.Equal(ka, kb)
}
