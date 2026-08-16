package net

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
)

// InstallTransport registers the leet:// scheme with go-git, so remotes
// like "leet://coordinator:7418/main.git" dial through mTLS with the
// node's identity (§6.4). tlsConf comes from Identity.TLSConfig.
func InstallTransport(tlsConf *tls.Config) {
	client.InstallProtocol(Scheme, &leetTransport{tls: tlsConf.Clone()})
}

// leetTransport implements transport.Transport: every session is one
// mTLS connection speaking the git pack protocol, exactly like the ssh
// transport's framing but encrypted and mutually authenticated by the
// team CA instead of host keys.
type leetTransport struct {
	tls *tls.Config
}

func (t *leetTransport) NewUploadPackSession(ep *transport.Endpoint, auth transport.AuthMethod) (transport.UploadPackSession, error) {
	return t.newSession(ep, auth, transport.UploadPackServiceName)
}

func (t *leetTransport) NewReceivePackSession(ep *transport.Endpoint, auth transport.AuthMethod) (transport.ReceivePackSession, error) {
	return t.newSession(ep, auth, transport.ReceivePackServiceName)
}

func (t *leetTransport) newSession(ep *transport.Endpoint, auth transport.AuthMethod, service string) (*leetSession, error) {
	// Authentication IS the mTLS identity (D8); there is nothing to pass.
	if auth != nil {
		return nil, transport.ErrInvalidAuthMethod
	}
	port := ep.Port
	if port == 0 {
		port = DefaultPort
	}
	hostPort := net.JoinHostPort(ep.Host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: handshakeTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", hostPort, t.tls)
	if err != nil {
		return nil, fmt.Errorf("net: leet dial %s: %w", hostPort, err)
	}
	// Bound the whole session, mirroring the server side, so a dead peer
	// cannot hang the sync loop.
	_ = conn.SetDeadline(time.Now().Add(connTimeout))
	// Same request line the git:// protocol uses: service, path, host.
	req := packp.GitProtoRequest{RequestCommand: service, Pathname: ep.Path, Host: hostPort}
	if err := req.Encode(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return &leetSession{conn: conn, isReceivePack: service == transport.ReceivePackServiceName}, nil
}

// leetSession is one leet:// connection. It satisfies both the fetch
// (UploadPack) and push (ReceivePack) session interfaces of go-git.
type leetSession struct {
	conn          *tls.Conn
	isReceivePack bool
	advRefs       *packp.AdvRefs
	packRun       bool
	finished      bool

	mu     sync.Mutex
	closed bool
}

// AdvertisedReferences retrieves the server's advertised references.
func (s *leetSession) AdvertisedReferences() (*packp.AdvRefs, error) {
	return s.AdvertisedReferencesContext(context.TODO())
}

// AdvertisedReferencesContext retrieves the advertised references for a
// repository, translating the git ERR line into a transport error.
func (s *leetSession) AdvertisedReferencesContext(ctx context.Context) (*packp.AdvRefs, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.advRefs != nil {
		return s.advRefs, nil
	}
	ar := packp.NewAdvRefs()
	if err := ar.Decode(s.conn); err != nil {
		var errLine *pktline.ErrorLine
		switch {
		case errors.As(err, &errLine):
			return nil, transport.ErrRepositoryNotFound
		case errors.Is(err, packp.ErrEmptyInput):
			return nil, fmt.Errorf("net: leet connection closed before advertisement: %w", err)
		case errors.Is(err, packp.ErrEmptyAdvRefs):
			if !s.isReceivePack {
				// Fetching from an empty repository is "nothing to
				// fetch"; pushing to one is fine and happens below.
				return nil, transport.ErrEmptyRemoteRepository
			}
		default:
			return nil, err
		}
	}
	if !s.isReceivePack && ar.IsEmpty() {
		return nil, transport.ErrEmptyRemoteRepository
	}
	transport.FilterUnsupportedCapabilities(ar.Capabilities)
	s.advRefs = ar
	return ar, nil
}

// UploadPack performs a fetch: send wants/haves, half-close, then decode
// the response with its packfile off the same connection.
func (s *leetSession) UploadPack(ctx context.Context, req *packp.UploadPackRequest) (*packp.UploadPackResponse, error) {
	if req.IsEmpty() {
		// Everything wanted is already local: politely hang up.
		if err := s.finish(); err != nil {
			return nil, err
		}
		return nil, transport.ErrEmptyUploadPackRequest
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.AdvertisedReferencesContext(ctx); err != nil {
		return nil, err
	}
	s.packRun = true

	if err := req.UploadRequest.Encode(s.conn); err != nil {
		return nil, fmt.Errorf("net: sending upload-request: %w", err)
	}
	if err := req.UploadHaves.Encode(s.conn, true); err != nil {
		return nil, fmt.Errorf("net: sending haves: %w", err)
	}
	if err := pktline.NewEncoder(s.conn).EncodeString("done\n"); err != nil {
		return nil, fmt.Errorf("net: sending done: %w", err)
	}
	if err := s.conn.CloseWrite(); err != nil {
		return nil, err
	}
	res := packp.NewUploadPackResponse(req)
	if err := res.Decode(s.conn); err != nil {
		return nil, fmt.Errorf("net: decoding upload-pack response: %w", err)
	}
	return res, nil
}

// ReceivePack performs a push: stream the reference-update request and its
// packfile, half-close, then read the report status.
func (s *leetSession) ReceivePack(ctx context.Context, req *packp.ReferenceUpdateRequest) (*packp.ReportStatus, error) {
	if _, err := s.AdvertisedReferencesContext(ctx); err != nil {
		return nil, err
	}
	s.packRun = true
	if err := req.Encode(s.conn); err != nil {
		return nil, fmt.Errorf("net: sending update request: %w", err)
	}
	if err := s.conn.CloseWrite(); err != nil {
		return nil, err
	}
	if !req.Capabilities.Supports(capability.ReportStatus) {
		return nil, s.Close()
	}
	report := packp.NewReportStatus()
	if err := report.Decode(s.conn); err != nil {
		return nil, fmt.Errorf("net: decoding report-status: %w", err)
	}
	if err := report.Error(); err != nil {
		defer s.Close()
		return report, err
	}
	return report, s.Close()
}

// finish sends a flush-pkt when no pack exchange happened, so the server's
// read loop terminates cleanly (mirrors git's own hang-up).
func (s *leetSession) finish() error {
	if s.finished {
		return nil
	}
	s.finished = true
	if !s.packRun {
		return pktline.NewEncoder(s.conn).Flush()
	}
	return nil
}

// Close ends the session; safe to call again after the response reader
// has already closed the connection.
func (s *leetSession) Close() error {
	err := s.finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if cerr := s.conn.Close(); err == nil && !errors.Is(cerr, net.ErrClosed) {
		err = cerr
	}
	return err
}
