// Node networking for the daemon (Phase 3): coordinator serves the git
// transport + enrollment over TLS and announces itself via mDNS (§6); clients
// install the leet:// transport so the configured main share just works.
package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"leetoffice/internal/config"
	leetNet "leetoffice/internal/net"
)

// StartNetworking brings up the node's network role. Coordinators listen
// (git-over-mTLS + enrollment + mDNS announce); clients announce and install
// the leet:// transport if their main share is remote.
func (n *Node) StartNetworking(ctx context.Context) error {
	if n.Cfg.IsCoordinator() {
		return n.serveCoordinator(ctx)
	}
	return n.startClient(ctx)
}

func (n *Node) serveCoordinator(ctx context.Context) error {
	idDir := n.Cfg.IdentityDir
	ca, err := leetNet.OpenCA(filepath.Join(idDir, "ca"))
	if err != nil {
		ca, err = leetNet.CreateCA(filepath.Join(idDir, "ca"))
		if err != nil {
			return fmt.Errorf("team CA: %w", err)
		}
		log.Printf("net: created team CA (fingerprint %s) — keep it backed up", ca.Fingerprint())
	}
	coord, err := leetNet.LoadIdentity(idDir)
	if err != nil {
		coord, err = ca.Issue(n.Cfg.NodeID)
		if err != nil {
			return fmt.Errorf("coordinator identity: %w", err)
		}
		if err := coord.Save(idDir); err != nil {
			return err
		}
	}

	// git service over mTLS: the main share's bare repo
	bareRoot := bareRootFor(n.Cfg)
	if bareRoot == "" {
		return fmt.Errorf("coordinator main share must be a file:// path")
	}
	gitSrv, err := leetNet.ServeGit(n.Cfg.Listen.Git, coord.ServerTLSConfig(), bareRoot)
	if err != nil {
		return fmt.Errorf("git service: %w", err)
	}
	log.Printf("net: git service on %s (mTLS, bare root %s)", gitSrv.Addr(), bareRoot)

	// enrollment endpoint (D8): one-time secret → node cert
	enrollPort := 0
	if n.Cfg.EnrollmentSecret != "" {
		enr, err := leetNet.NewEnrollmentServer(ca, n.Cfg.EnrollmentSecret,
			n.Cfg.Listen.Enroll, coord.EnrollmentTLSConfig(), portOf(gitSrv.Addr()), shareRepoPath(n.Cfg))
		if err != nil {
			return fmt.Errorf("enrollment: %w", err)
		}
		log.Printf("net: enrollment on %s — nodes run `leetd enroll --coordinator %s --secret <secret>`",
			enr.Addr(), enrollAddr(enr.Addr(), n.Cfg))
		enrollPort = portOf(enr.Addr())
		n.enroll = enr
		go func() {
			<-ctx.Done()
			_ = enr.Close()
		}()
	}

	// mDNS announce (§6.2)
	ann, err := leetNet.Announce(n.Cfg.NodeID, "coordinator", ca.Fingerprint(), "",
		portOf(gitSrv.Addr()), enrollPort)
	if err != nil {
		log.Printf("net: mDNS announce failed (continuing): %v", err)
	} else {
		keepAnnounce(ctx, ann)
	}
	return nil
}

func (n *Node) startClient(ctx context.Context) error {
	fp := ""
	if strings.HasPrefix(n.Cfg.MainShare, leetNet.Scheme+"://") {
		id, err := leetNet.LoadIdentity(n.Cfg.IdentityDir)
		if err != nil {
			return fmt.Errorf("no node identity at %s — run `leetd enroll` first: %w", n.Cfg.IdentityDir, err)
		}
		leetNet.InstallTransport(id.TLSConfig())
		fp = id.Fingerprint()
	}
	// PresencePort is non-zero: hashicorp/mdns rejects port 0, which made
	// the old Announce(..., 0, 0) a guaranteed no-op.
	ann, err := leetNet.Announce(n.Cfg.NodeID, "client", fp, "", leetNet.PresencePort, 0)
	if err != nil {
		log.Printf("net: mDNS announce failed (continuing): %v", err)
		return nil
	}
	keepAnnounce(ctx, ann)
	return nil
}

// keepAnnounce holds an mDNS announcer until ctx is cancelled. startClient
// used to Announce then immediately Shutdown, so client presence never
// lasted (README: mDNS + recent-activity).
func keepAnnounce(ctx context.Context, ann interface{ Shutdown() error }) {
	if ann == nil {
		return
	}
	go func() {
		<-ctx.Done()
		_ = ann.Shutdown()
	}()
}

// bareRootFor is the directory ServeGit should treat as its root: the
// parent of the file:// share. ServeGit joins root + requested path
// (/main.git), so the root must be the folder that CONTAINS the bare
// repo — not the repo itself. v0.1.0 passed the repo path, and joiners
// looking for /main.git hit <share>/main.git → "repository not found".
func bareRootFor(cfg *config.Config) string {
	if !strings.HasPrefix(cfg.MainShare, "file://") {
		return ""
	}
	share := strings.TrimPrefix(cfg.MainShare, "file://")
	if share == "" {
		return ""
	}
	return filepath.Dir(share)
}

// shareRepoPath is the leet:// path enrollment advertises, derived from
// the coordinator's file:// share (main.git or the v0.1.0 main-share.git).
func shareRepoPath(cfg *config.Config) string {
	raw := strings.TrimPrefix(cfg.MainShare, "file://")
	name := filepath.Base(raw)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return leetNet.DefaultRepoPath
	}
	return leetNet.RepoPath(name)
}

func portOf(addr net.Addr) int {
	if a, ok := addr.(*net.TCPAddr); ok {
		return a.Port
	}
	return 0
}

func enrollAddr(addr net.Addr, cfg *config.Config) string {
	host := "this-host"
	if h, err := os.Hostname(); err == nil && h != "" {
		host = h
	}
	return net.JoinHostPort(host, fmt.Sprint(portOf(addr)))
}
