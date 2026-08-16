package net

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitserver "github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// Server-side timings: a slow handshake is a broken or hostile peer; a
// generous I/O deadline still bounds every connection.
const (
	handshakeTimeout = 30 * time.Second
	connTimeout      = 10 * time.Minute
	drainTimeout     = 5 * time.Second
)

// connReader hides Close, so the go-git server sessions (which close their
// packfile readers when finished) cannot drop the live mTLS connection.
type connReader struct{ io.Reader }

func (connReader) Close() error { return nil }

// GitServer serves the bare repositories under a root directory over the
// leet:// protocol (§6.4): the git pack protocol spoken inside a mutually
// authenticated TLS channel. The same go-git server sessions that back the
// in-process transports back this one — wrapped in TLS instead of a pipe.
type GitServer struct {
	ln        net.Listener
	tlsConf   *tls.Config
	srv       transport.Transport
	closeOnce sync.Once
}

// ServeGit starts listening on addr (use port 0 for an ephemeral port) and
// serves bare repos from bareRoot to any peer holding a CA-signed
// certificate (tlsConf from Identity.ServerTLSConfig).
func ServeGit(addr string, tlsConf *tls.Config, bareRoot string) (*GitServer, error) {
	root, err := filepath.Abs(bareRoot)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &GitServer{
		ln:      ln,
		tlsConf: tlsConf,
		srv:     gitserver.NewServer(repoLoader(root)),
	}
	go s.acceptLoop()
	return s, nil
}

// Addr reports the bound address (host:port for the leet:// remote URL).
func (s *GitServer) Addr() net.Addr { return s.ln.Addr() }

// Close stops the listener; in-flight connections finish their transfers.
func (s *GitServer) Close() error {
	var err error
	s.closeOnce.Do(func() { err = s.ln.Close() })
	return err
}

func (s *GitServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed, or unusable
		}
		go s.handle(conn)
	}
}

// handle runs one mTLS-wrapped git session. Rogue nodes die here: a peer
// without a CA-signed certificate never gets past the handshake (D8).
func (s *GitServer) handle(raw net.Conn) {
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(connTimeout))
	conn := tls.Server(raw, s.tlsConf)
	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	defer cancel()
	if err := conn.HandshakeContext(ctx); err != nil {
		return // rejected during handshake — no bytes of protocol leaked
	}

	var req packp.GitProtoRequest
	if err := req.Decode(conn); err != nil {
		return
	}
	repoPath, err := cleanRepoPath(req.Pathname)
	if err != nil {
		writeError(conn, err)
		return
	}
	ep := &transport.Endpoint{Protocol: Scheme, Path: repoPath}

	switch req.RequestCommand {
	case transport.UploadPackServiceName:
		_ = s.uploadPack(conn, ep)
	case transport.ReceivePackServiceName:
		_ = s.receivePack(conn, ep)
	default:
		writeError(conn, fmt.Errorf("unknown git service %q", req.RequestCommand))
	}

	// Drain the client's TLS half-close before raw.Close(): closing a
	// socket with unread bytes makes the kernel send an RST that can race
	// the client's final packfile/report reads (flaky "connection reset").
	_ = conn.SetReadDeadline(time.Now().Add(drainTimeout))
	_, _ = io.Copy(io.Discard, conn)
}

func (s *GitServer) uploadPack(conn *tls.Conn, ep *transport.Endpoint) error {
	sess, err := s.srv.NewUploadPackSession(ep, nil)
	if err != nil {
		writeError(conn, err) // repo not found and friends, pre-protocol
		return err
	}
	defer sess.Close()
	ar, err := sess.AdvertisedReferencesContext(context.Background())
	if err != nil {
		writeError(conn, err)
		return err
	}
	if err := ar.Encode(conn); err != nil {
		return err
	}
	req := packp.NewUploadPackRequest()
	if err := req.UploadRequest.Decode(conn); err != nil {
		// A flush-pkt or EOF right after the advertisement is git's
		// polite "nothing to fetch" hangup; no session state was touched.
		return nil
	}
	if err := decodeHaves(conn, &req.UploadHaves); err != nil {
		return err
	}
	res, err := sess.UploadPack(context.Background(), req)
	if err != nil {
		return err
	}
	// Encode streams the packfile from the server session's pipe and
	// closes it when done.
	return res.Encode(conn)
}

func (s *GitServer) receivePack(conn *tls.Conn, ep *transport.Endpoint) error {
	sess, err := s.srv.NewReceivePackSession(ep, nil)
	if err != nil {
		writeError(conn, err) // repo not found and friends, pre-protocol
		return err
	}
	defer sess.Close()
	ar, err := sess.AdvertisedReferencesContext(context.Background())
	if err != nil {
		writeError(conn, err)
		return err
	}
	if err := ar.Encode(conn); err != nil {
		return err
	}
	req := packp.NewReferenceUpdateRequest()
	// Decode stops after the commands; the packfile stays on the wire and
	// is drained by ReceivePack until the client's half-close.
	if err := req.Decode(conn); err != nil {
		return err // genuine garbage; a polite hangup is also fine here
	}
	// The go-git session CLOSES its Packfile reader when done draining —
	// hand it a reader whose Close is a no-op so the live connection
	// survives to carry the report status.
	req.Packfile = connReader{conn}
	report, err := sess.ReceivePack(context.Background(), req)
	if err != nil {
		return err
	}
	if report == nil {
		return nil // client did not ask for a report-status
	}
	return report.Encode(conn)
}

// decodeHaves reads the "have" pkt-lines following the upload-request,
// up to the terminating "done" (the client half-closes right after).
func decodeHaves(r io.Reader, h *packp.UploadHaves) error {
	sc := pktline.NewScanner(r)
	for sc.Scan() {
		b := sc.Bytes()
		if len(b) == 0 || b[0] == '#' { // comment/keepalive space
			continue
		}
		if bytes.Equal(b, pktline.Flush) {
			continue
		}
		line := strings.TrimSuffix(string(b), "\n")
		if line == "done" {
			return nil
		}
		hash, ok := strings.CutPrefix(line, "have ")
		if !ok {
			return fmt.Errorf("unexpected pkt-line %q in haves", line)
		}
		if len(hash) != 40 {
			return fmt.Errorf("malformed have hash %q", hash)
		}
		h.Haves = append(h.Haves, plumbing.NewHash(hash))
	}
	return sc.Err() // EOF without "done": client gave up, stop cleanly
}

// writeError sends a git ERR pkt-line so the client decodes a proper error.
func writeError(conn io.Writer, err error) {
	_ = (&pktline.ErrorLine{Text: err.Error()}).Encode(conn)
}

// cleanRepoPath normalizes the requested repository path ("/main.git") and
// keeps it inside the bare root: absolute, no traversal.
func cleanRepoPath(p string) (string, error) {
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("invalid repository path %q", p)
	}
	clean := path.Clean(p)
	if clean == "/" || strings.Contains(clean, "..") {
		return "", fmt.Errorf("invalid repository path %q", p)
	}
	return clean, nil
}

// repoLoader is the server-side loader feeding go-git's in-process server
// sessions: endpoint path -> bare repository under the root.
type repoLoader string

// Load returns a storer for the bare repo at the endpoint's path, or
// transport.ErrRepositoryNotFound when it does not exist.
func (l repoLoader) Load(ep *transport.Endpoint) (storer.Storer, error) {
	dir := filepath.Join(string(l), filepath.FromSlash(ep.Path))
	if _, err := os.Stat(filepath.Join(dir, "config")); err != nil {
		return nil, transport.ErrRepositoryNotFound
	}
	return filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault()), nil
}
